// Package testutil locates a reference happi.md for cross-runtime tests.
//
// It exists because the runtime path used to be a single hardcoded absolute
// path containing one developer's home directory. On every other machine the
// conformance suite skipped and the client suite simply FAILED — spawning a
// non-existent file yields an empty event stream, which reads as "the client
// is broken" rather than "the runtime is missing".
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// candidates returns the paths to try, most explicit first.
func candidates() []string {
	if fromEnv := os.Getenv("HAPPI_MD"); fromEnv != "" {
		return []string{fromEnv}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".hal", "happi.md"),
		filepath.Join(home, ".hal", "HAPPI.md"),
	}
}

// ReferenceRuntimePath returns a readable happi.md path, or "" if none.
func ReferenceRuntimePath() string {
	for _, path := range candidates() {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// RequireReferenceRuntime returns the runtime path, skipping the test when
// no runtime is reachable. Skipping is honest here: the absence of a local
// happi.md says nothing about whether hal-go is correct.
func RequireReferenceRuntime(t *testing.T) string {
	t.Helper()
	path := ReferenceRuntimePath()
	if path == "" {
		t.Skip("no reference happi.md found; set HAPPI_MD=/path/to/happi.md " +
			"(fetch it from the published gist) to enable cross-runtime tests")
	}
	return path
}
