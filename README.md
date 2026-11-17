# hal-go

HAPPI/1.3 client library for Go. AI as a syscall.

## Status

v0.3.0 — tracks `happi/1.3`. Zero external dependencies. Stdlib only.

## What this is

`hal-go` is the idiomatic Go client for the [HAPPI protocol](https://gist.github.com/LaurieScheepers/2483d5c218c21ecc931130bcee7dee83). HAPPI is a polyglot self-bootstrapping single-file protocol that treats AI inference as a Unix-style syscall: write a JSON envelope to stdin, read NDJSON events from stdout.

This library lets a Go program dispatch HAPPI envelopes through any HAPPI runtime (the canonical `bash happi.md run`, `hal --happi-api`, this repo's own `cmd/hal-go`, or any future runtime that honours the contract) and consume the typed event stream with channels and `context.Context` cancellation.

## Protocol versions

`hal-go` **emits** `happi/1.3` and **accepts** `happi/1.0` through `happi/1.3`.

A runtime always stamps events with *its own* version, never the envelope's. So a `happi/1.0` request sent to a `happi/1.3` runtime comes back as `happi/1.3` events — meaning a client that checks for exact version equality can only ever talk to one runtime build. Accepting the whole supported range is what makes the client interoperable; versions outside it are still rejected.

The vocabulary lives in one place, `happi.AcceptedVersions`, so the envelope and events packages cannot drift apart.

## Layout

```
happi/           Shared protocol version vocabulary (emit vs accept)
envelope/        HAPPI envelope schema, marshal/unmarshal
events/          Typed event structs + NDJSON streaming Decoder
cite/            v1.3 cite.verify — deterministic citation provenance
client/          Dispatch shim — context-aware exec.Cmd wrapper
providers/       Provider adapters mapping vendor APIs to HAPPI events
  anthropic/     Anthropic stdlib-shim adapter (in-memory Stream interface)
cmd/hal-go/      Reference runtime binary — `bash happi.md run` byte-compatible
conformance_test.go   Verifies the Go client agrees with the bash reference runtime
happi13_test.go       Version negotiation, idr/context records, cite.verify
```

## Event types

Seven streaming types — `started`, `delta`, `tool_call`, `tool_result`, `sub_request`, `completed`, `error` — plus two non-streaming records added in v1.1:

| Type | Carries | Answers |
|------|---------|---------|
| `IDR` | `idr_ref` | "What exactly happened in this run?" |
| `Context` | `context_ref` | "What is the identity of this belief, regardless of when it was recorded?" |

They are emitted *after* the outcome event, so `completed`/`error` is not always the last event in a stream. Use `events.IsOutcome` and `events.IsPostTerminator` to tell them apart.

## Citation provenance (v1.3)

`cite.verify` checks that every cited quote is verbatim-present in its source. The check is a deterministic string match, so a fabricated quote can never earn `verified` — it resolves to `not_found`.

```go
env, err := cite.New("cv-1",
    []cite.Source{{ID: "s1", Text: "The Act bars the claim after six years."}},
    []cite.Citation{
        {ID: "c1", SourceID: "s1", Quote: "bars the claim after six years"},   // real
        {ID: "c2", SourceID: "s1", Quote: "bars the claim after three years"}, // invented
    }, false)

// dispatch, then on the completed event:
p, _ := cite.DecodeProvenance(completed.Usage)
fmt.Println(p.GroundingRate)  // 0.5
fmt.Println(p.NotFound())     // [{c2 s1 not_found -1 -1}]
```

Pass `strict = true` to make the runtime exit non-zero on any unverified citation — a build gate. Note what this proves: the quote really is in the *supplied* source. It does **not** prove the source is authentic, nor that the quote supports the claim.

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

## Design choices

- **Zero external deps**: the core (envelope, events, client) imports nothing outside the Go standard library. The Anthropic adapter ships a stdlib-only `Stream` interface; the official `anthropic-sdk-go` integration is deferred to a future release so the dependency graph stays empty for callers who only need HAPPI dispatch.
- **`context.Context`-first**: every dispatch takes a context. Cancellation kills the runtime subprocess via `exec.CommandContext` and closes the event channel.
- **Syscall doctrine**: the `Client` is a typed shim. Switching the underlying runtime is a one-line change to the `Runtime` argv. Provider differences live behind the `providers/*/Adapter` boundary.
- **DRY/KISS-clean**: every function is under 50 lines, every cyclomatic complexity is under 15. Extraction helpers exist where the natural body would have crossed those ceilings.

## Tests

```sh
go test ./...
```

Cross-runtime tests resolve a reference `happi.md` from `$HAPPI_MD`, then the local install, and skip cleanly when none is reachable. Fetch the published reference to enable them anywhere:

```sh
curl -fsSL "https://gist.githubusercontent.com/LaurieScheepers/2483d5c218c21ecc931130bcee7dee83/raw/happi.md" -o /tmp/happi.md
HAPPI_MD=/tmp/happi.md go test ./...
```

## Licence

Apache-2.0. See [LICENSE](LICENSE).

Copyright (C) 2024-2026 Lourens Cornelius Scheepers / CodeTonight (Pty) Ltd

You may use, modify and redistribute this library in any project, open or
closed source, under the Apache License, version 2.0, which includes an
express patent grant. Earlier releases were published under AGPL-3.0-only.
