// Command hal-go is a minimal HAPPI/1.0 reference runtime in Go.
//
// It reads a single HAPPI envelope from stdin, dispatches by cmd, and
// emits NDJSON events on stdout. Two cmds are wired in v0.1:
//
//	version            -> emits started, delta("happi/1.0"), completed
//	echo               -> emits started, delta(args[i]) for each arg, completed
//	(anything else)    -> emits started, error(unsupported_cmd)
//
// Exit code: 0 on completed, 1 on error. Designed to be byte-compatible
// with bash happi.md run for the same envelopes.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/CodeTonight-SA/hal-go/envelope"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run reads one envelope from in, writes NDJSON events to out. Returns
// nil on completed, non-nil on terminal error event (caller exits 1).
func run(in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return emitError(out, "", "read_error", err.Error())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return emitError(out, "", "parse_error", err.Error())
	}
	if err := env.Validate(); err != nil {
		return emitError(out, env.ID, "parse_error", err.Error())
	}
	emit(out, env.ID, "started", nil)
	switch env.Cmd {
	case "version":
		emit(out, env.ID, "delta", map[string]any{"text": envelope.Version})
	case "echo":
		for _, a := range env.Args {
			emit(out, env.ID, "delta", map[string]any{"text": fmt.Sprint(a)})
		}
	default:
		return emitError(out, env.ID, "unsupported_cmd", "cmd not implemented: "+env.Cmd)
	}
	emit(out, env.ID, "completed", nil)
	return nil
}

// emit writes one NDJSON event line. Extra fields merge into the top
// of the JSON object.
func emit(out io.Writer, id, typ string, extra map[string]any) {
	rec := map[string]any{"v": envelope.Version, "id": id, "type": typ, "ts": 0}
	for k, v := range extra {
		rec[k] = v
	}
	b, _ := json.Marshal(rec)
	fmt.Fprintln(out, string(b))
}

// emitError writes a terminal error event and returns a sentinel error
// so main() exits non-zero.
func emitError(out io.Writer, id, code, msg string) error {
	emit(out, id, "error", map[string]any{"code": code, "message": msg})
	return fmt.Errorf("%s: %s", code, msg)
}
