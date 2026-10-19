package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openSQLiteRegressionDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "regression.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestChatMentionsLiteralSpecialCharacters(t *testing.T) {
	d := openSQLiteRegressionDB(t)
	for i, special := range []string{"_", "%", `\`, `\_%`} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			name := fmt.Sprintf("引用%d甲%s乙", i, special)
			decoy := strings.ReplaceAll(name, special, "任意字符")
			for _, text := range []string{name, decoy} {
				if _, _, err := d.Companies().UpsertCompany(text, ""); err != nil {
					t.Fatal(err)
				}
				if _, err := d.AddFinding(0, 0, "xss", text, "low", "", "", "test", nil); err != nil {
					t.Fatal(err)
				}
				if _, err := d.Exec(`INSERT INTO assets(type,app_name,bundle_id) VALUES('app',$1,$1)`, text); err != nil {
					t.Fatal(err)
				}
			}
			for _, kind := range []string{"company", "finding", "asset", ""} {
				items, err := d.SearchChatMentions(t.Context(), kind, name)
				want := 1
				if kind == "" {
					want = 3
				}
				if err != nil || len(items) != want {
					t.Fatalf("%s search %q: %+v, %v", kind, name, items, err)
				}
				for _, item := range items {
					if item.Label != name {
						t.Fatalf("matched wildcard decoy: %+v", item)
					}
				}
			}
			// Exercise the company's nkey branch independently of its name.
			if _, err := d.Exec(`UPDATE companies SET name='renamed' WHERE nkey=$1`, name); err != nil {
				t.Fatal(err)
			}
			if items, err := d.SearchChatMentions(t.Context(), "company", name); err != nil || len(items) != 1 {
				t.Fatalf("nkey search: %+v, %v", items, err)
			}
		})
	}
}

func TestSQLiteUpdateReturningTimestamps(t *testing.T) {
	d := openSQLiteRegressionDB(t)
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	t.Run("template", func(t *testing.T) {
		created, err := d.CreateTaskTemplate(TaskTemplateInput{Name: "timestamp", Description: "before", Goal: "goal"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(`UPDATE task_templates SET updated_at=$2 WHERE id=$1`, created.ID, old); err != nil {
			t.Fatal(err)
		}
		description := "after"
		updated, err := d.PatchTaskTemplate(created.ID, TaskTemplatePatch{Description: &description})
		if err != nil {
			t.Fatal(err)
		}
		stored, err := d.GetTaskTemplate(created.ID)
		if err != nil || stored == nil {
			t.Fatalf("get template: %+v, %v", stored, err)
		}
		if !updated.UpdatedAt.After(old) || !updated.UpdatedAt.Equal(stored.UpdatedAt) {
			t.Fatalf("returned %s, stored %s, old %s", updated.UpdatedAt, stored.UpdatedAt, old)
		}
	})
	t.Run("intercept", func(t *testing.T) {
		created, err := d.CreateInterceptRule("before", "tool_name", "string", "Write", "ask", "", 1, true, false, 0, "deny")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(`UPDATE intercept_rules SET updated_at=$2 WHERE id=$1`, created.ID, old); err != nil {
			t.Fatal(err)
		}
		updated, err := d.UpdateInterceptRule(created.ID, "after", "tool_name", "string", "Write", "ask", "", 1, true, false, 0, "deny")
		if err != nil {
			t.Fatal(err)
		}
		var stored time.Time
		if err := d.QueryRow(`SELECT updated_at FROM intercept_rules WHERE id=$1`, created.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !updated.UpdatedAt.After(old) || !updated.UpdatedAt.Equal(stored) {
			t.Fatalf("returned %s, stored %s, old %s", updated.UpdatedAt, stored, old)
		}
	})
}
