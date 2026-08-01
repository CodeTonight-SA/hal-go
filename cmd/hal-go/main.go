// Command hal-go is a HAPPI/1.3 runtime in Go.
//
// It reads a single HAPPI envelope from stdin, dispatches by cmd, and emits
// an NDJSON event stream on stdout. Exit code is 0 when the stream ends in
// completed and 1 when it ends in error.
//
// Commands: version, echo, spec.describe, envelope.validate, idr.emit,
// context.append, cite.verify. Anything else answers with unsupported_cmd.
//
// The dispatch logic lives in package runtime so it can be tested against an
// in-memory writer rather than by spawning a process for every case. This
// file stays deliberately thin: read stdin, dispatch, map the result onto an
// exit code.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/CodeTonight-SA/hal-go/runtime"
)

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read_error:", err)
		os.Exit(1)
	}
	if err := runtime.Dispatch(raw, os.Stdout); err != nil {
		// The error event is already on stdout; stderr carries the same
		// summary for a human watching the terminal.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
