package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLitePathPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("ARTEX_CONFIG", path)
	t.Setenv("ARTEX_DB_PATH", "")
	if err := os.WriteFile(path, []byte(`{"database":{"path":"configured.sqlite"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got, _, err := SQLitePath(); err != nil || got != "configured.sqlite" {
		t.Fatalf("configured=%q err=%v", got, err)
	}
	t.Setenv("ARTEX_DB_PATH", "environment.sqlite")
	if got, _, err := SQLitePath(); err != nil || got != "environment.sqlite" {
		t.Fatalf("environment=%q err=%v", got, err)
	}
	t.Setenv("ARTEX_DB_PATH", "")
	t.Setenv("ARTEX_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	if got, _, err := SQLitePath(); err != nil || filepath.Base(got) != "artex.sqlite" {
		t.Fatalf("default=%q err=%v", got, err)
	}
}
