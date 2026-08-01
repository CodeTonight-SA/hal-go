// Package runtime implements a HAPPI/1.3 runtime: one envelope in on stdin,
// one NDJSON event stream out on stdout.
//
// It lives in its own package rather than inside cmd/hal-go so the dispatch
// logic can be tested directly against an in-memory writer, without spawning
// a process for every case. cmd/hal-go is a thin wrapper over Dispatch.
//
// One deliberate divergence from the reference runtime: for context.append
// the context event is emitted AFTER the outcome event. The reference emits
// it before. HAPPI.md is explicit that after is correct — it calls context "a
// non-streaming terminator emitted after completed/error" and names the
// reverse order as a falsifier of the design. Following the recorded fixture
// would mean writing a known spec violation into another implementation, so
// this runtime fails that one fixture instead. Tracked as hal-conformance
// issue #2.
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/CodeTonight-SA/hal-go/cite"
	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/happi"
)

// session carries the per-dispatch state: where events go, and the audit
// buffer that flags.audit turns into an idr record.
type session struct {
	out     io.Writer
	audit   bool
	models  []string
	emitted []string
}

// emit writes one NDJSON event line, merging extra fields into the object.
func (s *session) emit(id, typ string, extra map[string]any) {
	rec := map[string]any{"v": happi.Version, "id": id, "type": typ, "ts": 0}
	for k, v := range extra {
		rec[k] = v
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	fmt.Fprintln(s.out, string(line))
	if s.audit {
		s.emitted = append(s.emitted, string(line))
	}
}

func (s *session) delta(id, text string) {
	s.emit(id, "delta", map[string]any{"text": text})
}

func (s *session) completed(id string, usage map[string]any) {
	extra := map[string]any{}
	if usage != nil {
		extra["usage"] = usage
	}
	s.emit(id, "completed", extra)
}

// fail emits a terminal error event and returns a non-nil error so the caller
// exits non-zero. Every failure path goes through here, so an error event and
// a non-zero exit can never come apart.
func (s *session) fail(id, code, msg string) error {
	s.emit(id, "error", map[string]any{"code": code, "message": msg})
	return fmt.Errorf("%s: %s", code, msg)
}

// specLines describe THIS runtime. The command list necessarily differs
// between implementations, which is why the conformance suite compares this
// fixture by shape rather than by bytes.
var specLines = []string{
	"HAPPI/1.3 - Harnessed-AI Polyglot Protocol Interface",
	"One JSON envelope in (stdin), one NDJSON event stream out (stdout).",
	"Envelope: {v, id, cmd, args?, flags?, auth?}",
	"Events: started -> (delta|tool_call|tool_result|sub_request)* -> completed|error -> [idr]",
	"v1.1: flags.audit=true emits idr terminator with sha256 of envelope+events",
	"v1.1: cmd context.append emits a context terminator content-addressing a decision body (excl id/ts/audit)",
	"v1.3: cmd cite.verify deterministically checks each citation quote is verbatim in its source (un-fakeable provenance floor)",
	"Cmds (this runtime): version, echo, spec.describe, envelope.validate, idr.emit, context.append, cite.verify",
	"Implementation: hal-go  Reference: happi.md",
}

// handler runs one command. Returning an error means the stream already
// carries an error event and the process must exit non-zero.
type handler func(s *session, env envelope.Envelope) error

// commands is the dispatch table. A map keeps lookup O(1) and flat, so
// adding a command never lengthens a branch chain.
var commands = map[string]handler{
	"version":           cmdVersion,
	"echo":              cmdEcho,
	"spec.describe":     cmdSpecDescribe,
	"envelope.validate": cmdEnvelopeValidate,
	"idr.emit":          cmdIdrEmit,
	"context.append":    cmdContextAppend,
	cite.Cmd:            cmdCiteVerify,
}

// cmdVersion reports the version of THIS runtime, never the envelope's.
// A runtime that echoed the envelope's version back would turn every version
// check into a tautology and hide a mismatch rather than expose it.
func cmdVersion(s *session, env envelope.Envelope) error {
	s.delta(env.ID, happi.Version)
	s.completed(env.ID, nil)
	return nil
}

func cmdEcho(s *session, env envelope.Envelope) error {
	for _, arg := range env.Args {
		s.delta(env.ID, fmt.Sprint(arg))
	}
	s.completed(env.ID, nil)
	return nil
}

func cmdSpecDescribe(s *session, env envelope.Envelope) error {
	for _, line := range specLines {
		s.delta(env.ID, line)
	}
	s.completed(env.ID, nil)
	return nil
}

// cmdEnvelopeValidate succeeds by construction: Dispatch validates before
// command lookup, so reaching this handler already proves the envelope is
// well formed.
func cmdEnvelopeValidate(s *session, env envelope.Envelope) error {
	s.delta(env.ID, "envelope OK: passed schema validation")
	s.completed(env.ID, map[string]any{"validated": true})
	return nil
}

func cmdIdrEmit(s *session, env envelope.Envelope) error {
	s.completed(env.ID, nil)
	return nil
}

// ContentAddr is the content address of a decision body: sha256 over
// canonical JSON with sorted keys, excluding the volatile id, ts and audit
// fields.
//
// Byte-identical to the reference runtime and to hal-py for the same body.
// That equality is the entire point — it is what allows a memory chain
// written by one runtime to be recognised and de-duplicated by another. Two
// records with the same meaning but different id or timestamp share one
// address.
func ContentAddr(body map[string]any) (string, error) {
	addressed := make(map[string]any, len(body))
	for k, v := range body {
		switch k {
		case "id", "ts", "audit":
			continue
		}
		addressed[k] = v
	}
	canonical, err := canonicalJSON(addressed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// canonicalJSON serialises with sorted keys and no spaces, matching
// Python's json.dumps(sort_keys=True, separators=(",", ":")).
//
// encoding/json already sorts map keys and omits spaces, but it also escapes
// <, > and & as <, > and & by default, which Python does not.
// Left unchecked that difference alone would produce a different hash for any
// body containing those characters — a divergence that would appear only for
// certain content, which is the worst kind.
func canonicalJSON(value any) (string, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func cmdContextAppend(s *session, env envelope.Envelope) error {
	if len(env.Args) == 0 {
		return s.fail(env.ID, "parse_error",
			"context.append: requires args[0] = decision body JSON")
	}
	body, err := decodeBody(env.Args[0])
	if err != nil {
		return s.fail(env.ID, "parse_error", "context.append: "+err.Error())
	}
	addr, err := ContentAddr(body)
	if err != nil {
		return s.fail(env.ID, "runtime_error",
			"context.append: cannot address body: "+err.Error())
	}
	s.completed(env.ID, map[string]any{"content_addr": addr})
	s.emit(env.ID, "context", map[string]any{
		"context_ref": map[string]any{
			"sha256":              addr,
			"predecessor_context": env.Flags["predecessor_context"],
			"snapshot_ref":        env.Flags["snapshot_ref"],
			"kind":                contextKind(env.Flags),
			"model_versions":      stringList(env.Flags["model_versions"]),
		},
	})
	return nil
}

// decodeBody accepts the decision body either as a JSON string argument or as
// an already-decoded object, since both shapes reach a runtime in practice.
func decodeBody(arg any) (map[string]any, error) {
	if obj, ok := arg.(map[string]any); ok {
		return obj, nil
	}
	text, ok := arg.(string)
	if !ok {
		return nil, fmt.Errorf("decision body must be a JSON object")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return nil, fmt.Errorf("decision body must be a JSON object")
	}
	return body, nil
}

func contextKind(flags map[string]any) string {
	if kind, ok := flags["kind"].(string); ok && kind != "" {
		return kind
	}
	return "context-delta"
}

// stringList normalises a flag to []string, returning an empty (never nil)
// slice so it serialises as [] rather than null.
func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func cmdCiteVerify(s *session, env envelope.Envelope) error {
	sources, citations, err := citeInputs(env.Flags)
	if err != nil {
		return s.fail(env.ID, "parse_error", "cite.verify: "+err.Error())
	}
	results, tally := verifyAll(s, env.ID, citations, sources)
	grounded := tally["verified"] + tally["fuzzy"]
	record := map[string]any{
		"sources":        sourceDigests(sources),
		"citations":      results,
		"tally":          tally,
		"grounding_rate": roundTo3(float64(grounded) / float64(max(len(citations), 1))),
	}
	if strict, _ := env.Flags["strict"].(bool); strict && tally["not_found"] > 0 {
		return s.fail(env.ID, "runtime_error", fmt.Sprintf(
			"%d citation(s) NOT verbatim in source — unproven", tally["not_found"]))
	}
	s.completed(env.ID, record)
	return nil
}

// citeInputs pulls and validates the sources and citations from flags.
func citeInputs(flags map[string]any) (map[string]string, []map[string]any, error) {
	rawSources, ok := flags["sources"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("requires flags.sources[] and flags.citations[]")
	}
	rawCitations, ok := flags["citations"].([]any)
	if !ok {
		return nil, nil, fmt.Errorf("requires flags.sources[] and flags.citations[]")
	}
	sources := make(map[string]string, len(rawSources))
	for _, item := range rawSources {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("each source needs string id and text")
		}
		id, idOK := obj["id"].(string)
		text, textOK := obj["text"].(string)
		if !idOK || !textOK {
			return nil, nil, fmt.Errorf("each source needs string id and text")
		}
		sources[id] = text
	}
	citations := make([]map[string]any, 0, len(rawCitations))
	for _, item := range rawCitations {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("each citation needs string id, source_id, quote")
		}
		citations = append(citations, obj)
	}
	return sources, citations, nil
}

// verifyAll checks each citation, streaming one delta per result.
func verifyAll(s *session, id string, citations []map[string]any,
	sources map[string]string) ([]map[string]any, map[string]int) {
	tally := map[string]int{"verified": 0, "fuzzy": 0, "not_found": 0}
	results := make([]map[string]any, 0, len(citations))
	for _, citation := range citations {
		cid, _ := citation["id"].(string)
		sourceID, _ := citation["source_id"].(string)
		quote, _ := citation["quote"].(string)
		status, start, end := cite.StatusNotFound, -1, -1
		if text, known := sources[sourceID]; known {
			status, start, end = cite.VerifyQuote(quote, text)
		}
		tally[status]++
		results = append(results, map[string]any{
			"id": cid, "source_id": sourceID,
			"status": status, "start": start, "end": end,
		})
		s.delta(id, cid+" "+status)
	}
	return results, tally
}

func sourceDigests(sources map[string]string) map[string]any {
	digests := make(map[string]any, len(sources))
	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		sum := sha256.Sum256([]byte(sources[id]))
		digests[id] = map[string]any{
			"sha256": hex.EncodeToString(sum[:]),
			// Character count, not byte count — the reference and hal-py both
			// report code points, and a byte count would differ for any
			// non-ASCII source.
			"chars": len([]rune(sources[id])),
		}
	}
	return digests
}

// roundTo3 matches Python's round(x, 3) for the grounding rate, so the same
// inputs produce the same recorded number in every runtime.
func roundTo3(value float64) float64 {
	scaled := value * 1000
	rounded := float64(int64(scaled + 0.5))
	if scaled < 0 {
		rounded = float64(int64(scaled - 0.5))
	}
	return rounded / 1000
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// emitIdr appends the audit receipt: sha256 over the envelope bytes followed
// by every event line emitted, so an identical run always hashes identically.
func (s *session) emitIdr(id string, raw []byte) {
	digest := sha256.New()
	digest.Write(raw)
	for _, line := range s.emitted {
		digest.Write([]byte(line))
	}
	models := s.models
	if models == nil {
		models = []string{}
	}
	s.emit(id, "idr", map[string]any{
		"idr_ref": map[string]any{
			"sha256":         hex.EncodeToString(digest.Sum(nil)),
			"cid":            nil,
			"model_versions": models,
			"block_anchor":   nil,
		},
	})
}

// Dispatch runs one envelope, writing the event stream to out. It returns nil
// when the stream ended in completed, and a non-nil error when it ended in
// error, so a caller can map the result straight onto an exit code.
func Dispatch(raw []byte, out io.Writer) error {
	s := &session{out: out}
	var env envelope.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return s.fail("", "parse_error", err.Error())
	}
	if err := env.Validate(); err != nil {
		return s.fail(env.ID, "parse_error", err.Error())
	}
	if audit, _ := env.Flags["audit"].(bool); audit {
		s.audit = true
		s.models = stringList(env.Flags["model_versions"])
	}
	s.emit(env.ID, "started", nil)
	handle, known := commands[env.Cmd]
	var err error
	if !known {
		err = s.fail(env.ID, "unsupported_cmd", "cmd not implemented: "+env.Cmd)
	} else {
		err = handle(s, env)
	}
	if s.audit {
		s.emitIdr(env.ID, raw)
	}
	return err
}
