// Package client implements the HAPPI/1.0 dispatch shim: an envelope
// is written to the runtime's stdin, and a typed event channel is
// returned that streams the runtime's NDJSON stdout.
//
// The runtime is any subprocess that honours the HAPPI/1.0 contract.
// Reference: bash happi.md run, hal --happi-api, hal-go cmd/hal-go.
//
// Idiom: caller passes context.Context. On ctx cancellation the
// subprocess receives SIGKILL (via exec.CommandContext) and the
// event channel closes. Caller MUST drain the channel or cancel ctx
// to avoid leaking the dispatcher goroutine.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/events"
)

// Client dispatches HAPPI envelopes through a configured runtime.
// Runtime is the argv slice that exec.CommandContext receives, e.g.
// []string{"bash", "/path/to/happi.md", "run"}.
type Client struct {
	Runtime []string
}

// New returns a Client wired to the given runtime argv.
func New(runtime ...string) *Client {
	return &Client{Runtime: runtime}
}

// Dispatch writes env to the runtime's stdin and returns a channel of
// typed events streaming from stdout. If subprocess startup fails the
// error is returned synchronously. NDJSON parse errors mid-stream are
// surfaced as a synthetic *events.Error on the channel and the channel
// is closed.
//
// On ctx cancellation the subprocess is killed and the channel closes.
func (c *Client) Dispatch(ctx context.Context, env envelope.Envelope) (<-chan events.Event, error) {
	if err := env.Validate(); err != nil {
		return nil, err
	}
	if len(c.Runtime) == 0 {
		return nil, fmt.Errorf("client: empty Runtime; configure argv first")
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("client: marshal envelope: %w", err)
	}

	cmd := exec.CommandContext(ctx, c.Runtime[0], c.Runtime[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("client: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("client: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("client: start runtime: %w", err)
	}

	go writeAndClose(stdin, payload)

	out := make(chan events.Event, 16)
	go streamEvents(cmd, stdout, out)
	return out, nil
}

// writeAndClose pushes the envelope payload to stdin and closes the
// pipe so the runtime sees EOF and proceeds to dispatch.
func writeAndClose(stdin io.WriteCloser, payload []byte) {
	defer stdin.Close()
	_, _ = stdin.Write(payload)
	_, _ = stdin.Write([]byte{'\n'})
}

// streamEvents reads NDJSON from stdout, decodes each line into a typed
// event, and forwards on out. Closes out and waits for cmd before return.
// Extracted from Dispatch to keep complexity below the gocyclo ceiling.
func streamEvents(cmd *exec.Cmd, stdout io.ReadCloser, out chan<- events.Event) {
	defer close(out)
	defer cmd.Wait()
	dec := events.NewDecoder(stdout)
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			out <- &events.Error{Code: "parse_error", Message: err.Error()}
			return
		}
		out <- ev
	}
}
