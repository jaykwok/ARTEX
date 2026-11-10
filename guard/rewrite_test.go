package guard

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/intercept"
)

func TestDeferredReviewEditEnforcesActualArguments(t *testing.T) {
	for _, tc := range []struct {
		name, edited           string
		enabled, deny, blocked bool
	}{
		{"narrow", `{"target":"fixture","threads":2}`, true, false, false},
		{"disabled", `{"target":"fixture","threads":2}`, false, false, true},
		{"target_changed", `{"target":"different","threads":2}`, true, false, true},
		{"static_deny", `{"target":"fixture","threads":2}`, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), "fixture.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if _, err = d.Exec(`DELETE FROM intercept_rules`); err != nil {
				t.Fatal(err)
			}
			ic := intercept.New(d)
			if err = ic.SetJudgeConfig(intercept.JudgeConfig{Enabled: true, ReviewEdit: tc.enabled, FailAction: "deny"}); err != nil {
				t.Fatal(err)
			}
			ic.SetReviewer(func(context.Context, int64, string, intercept.ReviewInput) (intercept.Decision, error) {
				return intercept.Decision{Action: "allow", RuleCode: "R1", EditedArguments: json.RawMessage(tc.edited), Message: "fixture review"}, nil
			})
			if tc.deny {
				if _, err = d.CreateInterceptRule("deny edited", "tool_input", "string", `"threads":2`, "deny", "fixture deny", 100, true, false, 0, "deny"); err != nil {
					t.Fatal(err)
				}
			}
			g := NewWithInterceptor(ic)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			// A rewrite requiring human approval must block when the run is cancelled,
			// even if the model had originally said allow.
			if tc.blocked {
				cancel()
			}
			blocked, _, updated := g.Hooks().PreToolUse(ctx, "ExecuteExtraTool", []byte(`{"tool_name":"recipe__fixture","params":{"target":"fixture","threads":10}}`))
			if blocked != tc.blocked {
				t.Fatalf("blocked=%v updated=%s", blocked, updated)
			}
			if !blocked && (!strings.Contains(string(updated), `"threads":2`) || !strings.Contains(string(updated), `"tool_name":"recipe__fixture"`)) {
				t.Fatalf("edit was not passed to dispatcher: %s", updated)
			}
		})
	}
}
