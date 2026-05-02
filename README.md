# hal-go

HAPPI/1.0 client library for Go. AI as a syscall.

## Status

v0.1 — initial release. Zero external dependencies. Stdlib only.

## What this is

`hal-go` is the idiomatic Go client for the [HAPPI/1.0 protocol](https://gist.github.com/LaurieScheepers/2483d5c218c21ecc931130bcee7dee83). HAPPI is a polyglot self-bootstrapping single-file protocol that treats AI inference as a Unix-style syscall: write a JSON envelope to stdin, read NDJSON events from stdout.

This library lets a Go program dispatch HAPPI envelopes through any HAPPI/1.0 runtime (the canonical `bash happi.md run`, `hal --happi-api`, this repo's own `cmd/hal-go`, or any future runtime that honours the contract) and consume the typed event stream with channels and `context.Context` cancellation.

## Layout

```
envelope/        HAPPI/1.0 envelope schema, marshal/unmarshal
events/          Seven typed event structs + NDJSON streaming Decoder
client/          Dispatch shim — context-aware exec.Cmd wrapper
providers/       Provider adapters mapping vendor APIs to HAPPI events
  anthropic/     Anthropic stdlib-shim adapter (v0.1: in-memory Stream interface)
cmd/hal-go/      Reference runtime binary — `bash happi.md run` byte-compatible
conformance_test.go   Verifies the Go client agrees with the bash reference runtime
```

## Quick start

```go
import (
    "context"
    "github.com/CodeTonight-SA/hal-go/client"
    "github.com/CodeTonight-SA/hal-go/envelope"
    "github.com/CodeTonight-SA/hal-go/events"
)

c := client.New("bash", "/path/to/happi.md", "run")
env := envelope.New("req-1", "version")

stream, err := c.Dispatch(context.Background(), env)
if err != nil { panic(err) }

for ev := range stream {
    switch e := ev.(type) {
    case *events.Delta:
        print(e.Text)
    case *events.Error:
        return fmt.Errorf("%s: %s", e.Code, e.Message)
    }
}
```

## v0.1 design choices

- **Zero external deps**: the core (envelope, events, client) imports nothing outside the Go standard library. The Anthropic adapter ships a stdlib-only `Stream` interface; the official `anthropic-sdk-go` integration is deferred to v0.2 so the dependency graph stays empty for callers who only need HAPPI dispatch.
- **`context.Context`-first**: every dispatch takes a context. Cancellation kills the runtime subprocess via `exec.CommandContext` and closes the event channel.
- **Syscall doctrine**: the `Client` is a typed shim. Switching the underlying runtime is a one-line change to the `Runtime` argv. Provider differences live behind the `providers/*/Adapter` boundary.
- **DRY/KISS-clean**: every function is under 50 lines, every cyclomatic complexity is under 15. Extraction helpers exist where the natural body would have crossed those ceilings.

## Tests

```sh
go test ./...
```

The conformance test (`conformance_test.go`) skips cleanly if the reference runtime at `/Users/lauriescheepers/.hal/happi.md` is not present.

## License

MIT — see LICENSE.
