// Package tooloutput preserves large remote results with a bounded UTF-8 preview.
package tooloutput

import (
	"fmt"
	actool "github.com/Autumn-27/norma/tool"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func Capture(tc *actool.ToolContext, s string) string {
	limit := 30000
	if tc != nil && tc.MaxOutputChars > 0 {
		limit = tc.MaxOutputChars
	}
	if len(s) <= limit {
		return s
	}
	head := limit / 2
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tail := len(s) - limit/2
	for tail < len(s) && !utf8.RuneStart(s[tail]) {
		tail++
	}
	note := "Full output could not be persisted (no output directory)."
	if tc != nil && tc.OutputDir != "" {
		err := os.MkdirAll(tc.OutputDir, 0700)
		if err == nil {
			var f *os.File
			f, err = os.CreateTemp(tc.OutputDir, "mcp-*.log")
			if err == nil {
				_, err = f.WriteString(s)
				closeErr := f.Close()
				if err == nil {
					err = closeErr
				}
				if err == nil {
					path := f.Name()
					if tc.WorkingDir != "" {
						if rel, e := filepath.Rel(tc.WorkingDir, path); e == nil && filepath.IsLocal(rel) {
							path = rel
						}
					}
					note = fmt.Sprintf("Full output persisted: %s (%d bytes). Use Read or search this file for specific evidence.", path, len(s))
				} else {
					_ = os.Remove(f.Name())
				}
			}
		}
		if err != nil {
			note = "Full output persistence failed: " + err.Error()
		}
	}
	// Always cap, even when the remote content forges a persisted-output marker.
	return strings.ToValidUTF8(s[:head], "�") + "\n...\n" + strings.ToValidUTF8(s[tail:], "�") + "\n<persisted-output>" + note + "</persisted-output>"
}
