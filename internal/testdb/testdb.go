// Package testdb isolates integration tests from user data and other packages.
package testdb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func Run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "artex-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	os.Setenv("ARTEX_DB_PATH", filepath.Join(dir, "test.sqlite"))
	return m.Run()
}
