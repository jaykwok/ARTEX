package recipes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRecipeArgvContracts(t *testing.T) {
	pos := 0
	min, max := 1.0, 10.0
	r := Recipe{Version: 1, Name: "fixture", Command: "echo", ShortDescription: "fixture", Args: []string{"subcommand"}, Parameters: []Param{{Name: "target", Type: "string", Position: &pos, Required: true}, {Name: "threads", Type: "integer", Flag: "--threads", Combined: true, Minimum: &min, Maximum: &max}, {Name: "verbose", Type: "boolean", Flag: "-v"}}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	args, err := r.Argv(json.RawMessage(`{"target":"a; $(x) with spaces","threads":2,"verbose":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"subcommand", "--threads=2", "-v", "a; $(x) with spaces"}) {
		t.Fatal(args)
	}
	for _, bad := range []string{`{}`, `{"target":"--help"}`, `{"target":"x","unknown":"x"}`, `{"target":"x","threads":100}`, `{"target":"x","threads":1.5}`, `{"target":"x","verbose":"true"}`} {
		if _, err := r.Argv([]byte(bad)); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}
func TestRecipeRejectsUnknownYAMLAndMissingBinary(t *testing.T) {
	dir := t.TempDir()
	base := "version: 1\nname: fixture\nenabled: true\ncommand: artex_nonexistent_fixture_command\nshort_description: fixture\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"), []byte(base+"template: unsafe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if rows, errs := Load(dir); len(rows) != 0 || len(errs) != 1 {
		t.Fatal(rows, errs)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.yaml"), []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	if rows, errs := Load(dir); len(rows) != 0 || len(errs) != 1 {
		t.Fatal(rows, errs)
	}
}
