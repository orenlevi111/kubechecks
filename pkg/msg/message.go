package msg

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"

	"github.com/zapier/kubechecks/pkg"
)

type CheckResult struct {
	State            pkg.CommitState
	Summary, Details string
}

type AppResults struct {
	results []CheckResult
}

func (ar *AppResults) AddCheckResult(result CheckResult) {
	ar.results = append(ar.results, result)
}

func NewMessage(name string, prId, commentId int, vcs toEmoji) *Message {
	return &Message{
		Name:    name,
		CheckID: prId,
		NoteID:  commentId,
		vcs:     vcs,

		apps:           make(map[string]*AppResults),
		deletedAppsSet: make(map[string]struct{}),
	}
}

type toEmoji interface {
	ToEmoji(state pkg.CommitState) string
}

// Message type that allows concurrent updates
// Has a reference to the owner/repo (ie zapier/kubechecks),
// the PR/MR id, and the actual messsage
type Message struct {
	Name    string
	Owner   string
	CheckID int
	NoteID  int

	// Key = Appname, value = Results
	apps   map[string]*AppResults
	footer string
	lock   sync.Mutex
	vcs    toEmoji

	deletedAppsSet map[string]struct{}
}

func (m *Message) WorstState() pkg.CommitState {
	state := pkg.StateNone

	for app, r := range m.apps {
		if m.isDeleted(app) {
			continue
		}

		for _, result := range r.results {
			state = pkg.WorstState(state, result.State)
		}
	}

	return state
}

func (m *Message) RemoveApp(app string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.deletedAppsSet[app] = struct{}{}
}

func (m *Message) isDeleted(app string) bool {
	if _, ok := m.deletedAppsSet[app]; ok {
		return true
	}

	return false
}

func (m *Message) AddNewApp(ctx context.Context, app string) {
	if m.isDeleted(app) {
		return
	}

	_, span := otel.Tracer("Kubechecks").Start(ctx, "AddNewApp")
	defer span.End()
	m.lock.Lock()
	defer m.lock.Unlock()

	m.apps[app] = new(AppResults)
}

func (m *Message) AddToAppMessage(ctx context.Context, app string, result CheckResult) {
	if m.isDeleted(app) {
		return
	}

	_, span := otel.Tracer("Kubechecks").Start(ctx, "AddToAppMessage")
	defer span.End()
	m.lock.Lock()
	defer m.lock.Unlock()

	m.apps[app].AddCheckResult(result)
}

var hostname = ""

func init() {
	hostname, _ = os.Hostname()
}

func (m *Message) SetFooter(start time.Time, commitSHA, labelFilter string, showDebugInfo bool) {
	if !showDebugInfo {
		m.footer = fmt.Sprintf("<small>_Done. CommitSHA: %s_<small>\n", commitSHA)
		return
	}

	envStr := ""
	if labelFilter != "" {
		envStr = fmt.Sprintf(", Env: %s", labelFilter)
	}
	duration := time.Since(start)

	m.footer = fmt.Sprintf("<small>_Done: Pod: %s, Dur: %v, SHA: %s%s_<small>\n", hostname, duration, pkg.GitCommit, envStr)
}

func (m *Message) BuildComment(ctx context.Context) string {
	return m.buildComment(ctx)
}

// Iterate the map of all apps in this message, building a final comment from their current state
func (m *Message) buildComment(ctx context.Context) string {
	_, span := otel.Tracer("Kubechecks").Start(ctx, "buildComment")
	defer span.End()

	names := getSortedKeys(m.apps)

	var sb strings.Builder
	sb.WriteString("# Kubechecks Report\n")

	for _, appName := range names {
		if m.isDeleted(appName) {
			continue
		}

		var checkStrings []string
		results := m.apps[appName]

		appState := pkg.StateSuccess
		for _, check := range results.results {
			var summary string
			if check.State == pkg.StateNone {
				summary = check.Summary
			} else {
				summary = fmt.Sprintf("%s %s %s", check.Summary, check.State.BareString(), m.vcs.ToEmoji(check.State))
			}

			msg := fmt.Sprintf("<details>\n<summary>%s</summary>\n\n%s\n</details>", summary, check.Details)
			checkStrings = append(checkStrings, msg)
			appState = pkg.WorstState(appState, check.State)
		}

		sb.WriteString("<details>\n")
		sb.WriteString("<summary>\n\n")
		sb.WriteString(fmt.Sprintf("## ArgoCD Application Checks: `%s` %s\n", appName, m.vcs.ToEmoji(appState)))
		sb.WriteString("</summary>\n\n")
		sb.WriteString(strings.Join(checkStrings, "\n\n---\n\n"))
		sb.WriteString("</details>")
	}

	return sb.String()
}

func getSortedKeys[K constraints.Ordered, V any](m map[K]V) []K {
	var keys []K
	for key := range m {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}