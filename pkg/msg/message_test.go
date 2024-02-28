package msg

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zapier/kubechecks/pkg"
)

type fakeEmojiable struct {
	emoji string
}

func (fe fakeEmojiable) ToEmoji(state pkg.CommitState) string { return fe.emoji }

func TestBuildComment(t *testing.T) {
	appResults := map[string]*AppResults{
		"myapp": {
			results: []CheckResult{
				{
					State:   pkg.StateError,
					Summary: "this failed bigly",
					Details: "should add some important details here",
				},
			},
		},
	}
	m := NewMessage("message", 1, 2, fakeEmojiable{":test:"})
	m.apps = appResults
	comment := m.buildComment(context.TODO())
	assert.Equal(t, `# Kubechecks Report
<details>
<summary>

## ArgoCD Application Checks: `+"`myapp`"+` :test:
</summary>

<details>
<summary>this failed bigly Error :test:</summary>

should add some important details here
</details></details>`, comment)
}

func TestMessageIsSuccess(t *testing.T) {
	t.Run("logic works", func(t *testing.T) {
		var (
			message = NewMessage("name", 1, 2, fakeEmojiable{":test:"})
			ctx     = context.TODO()
		)

		// no apps mean success
		assert.Equal(t, pkg.StateNone, message.WorstState())

		// one app, no checks = success
		message.AddNewApp(ctx, "some-app")
		assert.Equal(t, pkg.StateNone, message.WorstState())

		// one app, one success = success
		message.AddToAppMessage(ctx, "some-app", CheckResult{State: pkg.StateSuccess})
		assert.Equal(t, pkg.StateSuccess, message.WorstState())

		// one app, one success, one failure = failure
		message.AddToAppMessage(ctx, "some-app", CheckResult{State: pkg.StateFailure})
		assert.Equal(t, pkg.StateFailure, message.WorstState())

		// one app, two successes, one failure = failure
		message.AddToAppMessage(ctx, "some-app", CheckResult{State: pkg.StateSuccess})
		assert.Equal(t, pkg.StateFailure, message.WorstState())

		// one app, two successes, one failure = failure
		message.AddToAppMessage(ctx, "some-app", CheckResult{State: pkg.StateSuccess})
		assert.Equal(t, pkg.StateFailure, message.WorstState())

		// two apps: second app's success does not override first app's failure
		message.AddNewApp(ctx, "some-other-app")
		message.AddToAppMessage(ctx, "some-other-app", CheckResult{State: pkg.StateSuccess})
		assert.Equal(t, pkg.StateFailure, message.WorstState())
	})

	testcases := map[pkg.CommitState]struct{}{
		pkg.StateNone:    {},
		pkg.StateSuccess: {},
		pkg.StateRunning: {},
		pkg.StateWarning: {},
		pkg.StateFailure: {},
		pkg.StateError:   {},
		pkg.StatePanic:   {},
	}

	for state := range testcases {
		t.Run(state.BareString(), func(t *testing.T) {
			var (
				message = NewMessage("name", 1, 2, fakeEmojiable{":test:"})
				ctx     = context.TODO()
			)
			message.AddNewApp(ctx, "some-app")
			message.AddToAppMessage(ctx, "some-app", CheckResult{State: state})
			assert.Equal(t, state, message.WorstState())
		})
	}
}

func TestMultipleItemsWithNewlines(t *testing.T) {
	var (
		message = NewMessage("name", 1, 2, fakeEmojiable{":test:"})
		ctx     = context.Background()
	)
	message.AddNewApp(ctx, "first-app")
	message.AddToAppMessage(ctx, "first-app", CheckResult{
		State:   pkg.StateSuccess,
		Summary: "summary-1",
		Details: "detail-1",
	})
	message.AddToAppMessage(ctx, "first-app", CheckResult{
		State:   pkg.StateSuccess,
		Summary: "summary-2",
		Details: "detail-2",
	})
	message.AddNewApp(ctx, "second-app")
	message.AddToAppMessage(ctx, "second-app", CheckResult{
		State:   pkg.StateSuccess,
		Summary: "summary-1",
		Details: "detail-1",
	})
	message.AddToAppMessage(ctx, "second-app", CheckResult{
		State:   pkg.StateSuccess,
		Summary: "summary-2",
		Details: "detail-2",
	})
	result := message.BuildComment(ctx)

	// header rows need double newlines before and after
	index := 0
	newline := uint8('\n')
	for {
		index++
		foundAt := strings.Index(result[index:], "---")
		if foundAt == -1 {
			break // couldn't be found, we're done
		}
		index += foundAt

		if index < 2 {
			continue // hyphens are at the beginning of the string, we're fine
		}

		if result[index-1] == '-' || result[index+3] == '-' {
			continue // not a triple-hyphen, but a more-than-triple-hyphen, move on
		}

		// must be preceded by two newlines
		assert.Equal(t, newline, result[index-1])
		assert.Equal(t, newline, result[index-2])

		// must be followed by two newlines
		assert.Equal(t, newline, result[index+3])
		assert.Equal(t, newline, result[index+4])
	}
}