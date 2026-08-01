// HAPPI/1.3 regression anchors: version negotiation, the v1.1 record
// terminators, and cite.verify.
//
// hal-go v0.1 compared the wire version for exact equality against a single
// "happi/1.0" constant in both envelope.Validate and events.parseLine. Once
// the reference runtime advanced to happi/1.3, every event it produced was
// rejected with "unsupported version". A runtime stamps events with its own
// version, never the envelope's, so exact equality can only ever work
// against one runtime build.
package halgo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CodeTonight-SA/hal-go/cite"
	"github.com/CodeTonight-SA/hal-go/client"
	"github.com/CodeTonight-SA/hal-go/envelope"
	"github.com/CodeTonight-SA/hal-go/events"
	"github.com/CodeTonight-SA/hal-go/happi"
)

var allVersions = []string{"happi/1.0", "happi/1.1", "happi/1.2", "happi/1.3"}

func TestEmittedVersionIs13(t *testing.T) {
	if happi.Version != "happi/1.3" {
		t.Errorf("happi.Version = %q, want \"happi/1.3\"", happi.Version)
	}
	if envelope.Version != "happi/1.3" || events.Version != "happi/1.3" {
		t.Errorf("envelope.Version=%q events.Version=%q, both want \"happi/1.3\"",
			envelope.Version, events.Version)
	}
}

func TestAcceptsEveryShippedVersion(t *testing.T) {
	// Named explicitly rather than read back from the package, so a mutation
	// that narrows acceptance fails instead of trivially agreeing with itself.
	for _, v := range allVersions {
		if !happi.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = false, want true", v)
		}
	}
}

func TestAcceptsFutureMinorsOfTheSameMajor(t *testing.T) {
	// The regression anchor for the council's blocking concern: a hardcoded
	// accept-list merely DEFERS the version bug to the next release. HAPPI
	// promises additive-only change within a major, so any happi/1.x must be
	// accepted — otherwise every SDK breaks again on the day 1.4 ships.
	for _, v := range []string{"happi/1.4", "happi/1.5", "happi/1.12", "happi/1.99"} {
		if !happi.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = false — a future minor must be accepted", v)
		}
	}
}

func TestRejectsOtherMajorsAndMalformed(t *testing.T) {
	// Forward compatibility is NOT "accept anything". A major bump is exactly
	// the signal that assumptions no longer hold, so it must still be refused.
	for _, v := range []string{
		"happi/2.0", "happi/0.9", "happi/2.1",
		"", "1.3", "happi/", "happi/1", "happi/x.y", "happi/1.x", "nothappi/1.3",
	} {
		if happi.IsAccepted(v) {
			t.Errorf("IsAccepted(%q) = true, want false", v)
		}
	}
}

func TestEnvelopeValidateAcceptsEverySupportedVersion(t *testing.T) {
	for _, v := range allVersions {
		env := envelope.Envelope{V: v, ID: "r", Cmd: "version"}
		if err := env.Validate(); err != nil {
			t.Errorf("Validate(%s): unexpected error %v", v, err)
		}
	}
}

func TestEnvelopeValidateRejectsOutOfRange(t *testing.T) {
	// Accepting any happi/1.x must not become "accept anything". A different
	// major, or a malformed version string, is still refused.
	// Note happi/1.4 is deliberately NOT here: a future MINOR is accepted by
	// design, which is the whole point of the major-version policy.
	for _, v := range []string{"happi/0.9", "happi/2.0", "", "1.3", "happi/1.x"} {
		env := envelope.Envelope{V: v, ID: "r", Cmd: "version"}
		if err := env.Validate(); err == nil {
			t.Errorf("Validate(%q): expected an error, got nil", v)
		}
	}
}

func TestEnvelopeValidateAcceptsAFutureMinor(t *testing.T) {
	env := envelope.Envelope{V: "happi/1.4", ID: "r", Cmd: "version"}
	if err := env.Validate(); err != nil {
		t.Errorf("Validate(happi/1.4): unexpected error %v — a future minor "+
			"must be accepted, or this SDK breaks again the day 1.4 ships", err)
	}
}

func TestDecoderAcceptsEverySupportedVersion(t *testing.T) {
	for _, v := range allVersions {
		line := `{"v":"` + v + `","id":"r","type":"started","ts":0}`
		d := events.NewDecoder(strings.NewReader(line + "\n"))
		ev, err := d.Next()
		if err != nil {
			t.Errorf("Next(%s): unexpected error %v", v, err)
			continue
		}
		if ev.EventType() != "started" {
			t.Errorf("Next(%s): type = %q, want \"started\"", v, ev.EventType())
		}
	}
}

func TestDecoderParsesA13RuntimeStream(t *testing.T) {
	// Verbatim shape of what `bash happi.md run` emits for cmd version.
	stream := `{"v":"happi/1.3","id":"probe","type":"started","ts":0}
{"v":"happi/1.3","id":"probe","type":"delta","ts":0,"text":"happi/1.3"}
{"v":"happi/1.3","id":"probe","type":"completed","ts":0}
`
	d := events.NewDecoder(strings.NewReader(stream))
	var got []string
	for {
		ev, err := d.Next()
		if err != nil {
			break
		}
		got = append(got, ev.EventType())
	}
	want := []string{"started", "delta", "completed"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("types = %v, want %v", got, want)
	}
}

func TestDecoderRejectsOutOfRangeVersion(t *testing.T) {
	d := events.NewDecoder(strings.NewReader(
		`{"v":"happi/0.9","id":"r","type":"started","ts":0}` + "\n"))
	if _, err := d.Next(); err == nil {
		t.Error("expected an error for happi/0.9, got nil")
	}
}

/* ── v1.1 record terminators ─────────────────────────────────────────────── */

const idrLine = `{"v":"happi/1.3","id":"r","type":"idr","ts":7,` +
	`"idr_ref":{"sha256":"abc123","cid":null,` +
	`"model_versions":["claude-opus-4-7"],"block_anchor":null}}`

const contextLine = `{"v":"happi/1.3","id":"r","type":"context","ts":8,` +
	`"context_ref":{"sha256":"sha256:def456","predecessor_context":"r-prev",` +
	`"snapshot_ref":null,"kind":"context-delta",` +
	`"model_versions":["claude-opus-4-7"]}}`

func TestDecodeIDREvent(t *testing.T) {
	d := events.NewDecoder(strings.NewReader(idrLine + "\n"))
	ev, err := d.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	idr, ok := ev.(*events.IDR)
	if !ok {
		t.Fatalf("event type = %T, want *events.IDR", ev)
	}
	if idr.Ref.SHA256 != "abc123" {
		t.Errorf("SHA256 = %q, want \"abc123\"", idr.Ref.SHA256)
	}
	if len(idr.Ref.ModelVersions) != 1 || idr.Ref.ModelVersions[0] != "claude-opus-4-7" {
		t.Errorf("ModelVersions = %v, want [claude-opus-4-7]", idr.Ref.ModelVersions)
	}
}

func TestDecodeContextEvent(t *testing.T) {
	d := events.NewDecoder(strings.NewReader(contextLine + "\n"))
	ev, err := d.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	ctxEv, ok := ev.(*events.Context)
	if !ok {
		t.Fatalf("event type = %T, want *events.Context", ev)
	}
	if ctxEv.Ref.Kind != events.KindContextDelta {
		t.Errorf("Kind = %q, want %q", ctxEv.Ref.Kind, events.KindContextDelta)
	}
	if ctxEv.Ref.PredecessorContext == nil || *ctxEv.Ref.PredecessorContext != "r-prev" {
		t.Errorf("PredecessorContext = %v, want \"r-prev\"", ctxEv.Ref.PredecessorContext)
	}
}

func TestTerminatorClassification(t *testing.T) {
	d := events.NewDecoder(strings.NewReader(
		`{"v":"happi/1.3","id":"r","type":"completed","ts":5}` + "\n" + idrLine + "\n"))

	outcome, err := d.Next()
	if err != nil {
		t.Fatalf("Next(outcome): %v", err)
	}
	idr, err := d.Next()
	if err != nil {
		t.Fatalf("Next(idr): %v", err)
	}
	if !events.IsOutcome(outcome) || events.IsPostTerminator(outcome) {
		t.Error("completed should be an outcome, not a post-terminator")
	}
	if events.IsOutcome(idr) || !events.IsPostTerminator(idr) {
		t.Error("idr should be a post-terminator, not an outcome")
	}
}

/* ── v1.3 cite.verify ────────────────────────────────────────────────────── */

var (
	citeSource = cite.Source{
		ID:   "s1",
		Text: "The Limitation Act 1980 bars the claim after six years.",
	}
	citeReal = cite.Citation{ID: "c1", SourceID: "s1", Quote: "bars the claim after six years"}
	citeFake = cite.Citation{ID: "c2", SourceID: "s1", Quote: "bars the claim after three years"}
)

func TestCiteVerifyEnvelopeShape(t *testing.T) {
	env, err := cite.New("cv-1", []cite.Source{citeSource},
		[]cite.Citation{citeReal, citeFake}, false)
	if err != nil {
		t.Fatalf("cite.New: %v", err)
	}
	if env.Cmd != "cite.verify" {
		t.Errorf("Cmd = %q, want \"cite.verify\"", env.Cmd)
	}
	if env.V != "happi/1.3" {
		t.Errorf("V = %q, want \"happi/1.3\"", env.V)
	}
	if _, present := env.Flags["strict"]; present {
		t.Error("strict must be absent by default; the caller reads grounding_rate")
	}
}

func TestCiteVerifyStrictFlag(t *testing.T) {
	env, err := cite.New("cv-2", []cite.Source{citeSource}, []cite.Citation{citeReal}, true)
	if err != nil {
		t.Fatalf("cite.New: %v", err)
	}
	if env.Flags["strict"] != true {
		t.Errorf("strict = %v, want true", env.Flags["strict"])
	}
}

func TestCiteVerifyRejectsEmptyInputs(t *testing.T) {
	if _, err := cite.New("x", nil, []cite.Citation{citeReal}, false); err == nil {
		t.Error("expected an error for zero sources")
	}
	if _, err := cite.New("x", []cite.Source{citeSource}, nil, false); err == nil {
		t.Error("expected an error for zero citations")
	}
}

func TestDecodeProvenanceSeparatesFabricatedCitations(t *testing.T) {
	var usage map[string]any
	raw := `{"sources":{"s1":{"sha256":"deadbeef","chars":55}},
	 "citations":[{"id":"c1","source_id":"s1","status":"verified","start":24,"end":54},
	              {"id":"c2","source_id":"s1","status":"not_found","start":-1,"end":-1}],
	 "tally":{"verified":1,"fuzzy":0,"not_found":1},"grounding_rate":0.5}`
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	p, err := cite.DecodeProvenance(usage)
	if err != nil {
		t.Fatalf("DecodeProvenance: %v", err)
	}
	if p.GroundingRate != 0.5 {
		t.Errorf("GroundingRate = %v, want 0.5", p.GroundingRate)
	}
	if len(p.NotFound()) != 1 || p.NotFound()[0].ID != "c2" {
		t.Errorf("NotFound = %v, want [c2]", p.NotFound())
	}
	if len(p.Verified()) != 1 || !p.Verified()[0].Grounded() {
		t.Errorf("Verified = %v, want one grounded result", p.Verified())
	}
	if p.FullyGrounded() {
		t.Error("FullyGrounded must be false when a citation is not_found")
	}
}

func TestEmptyProvenanceIsNotGrounded(t *testing.T) {
	// Nothing was proven, so nothing is grounded.
	if (cite.Provenance{}).FullyGrounded() {
		t.Error("an empty provenance record must not report as fully grounded")
	}
}

func TestDecodeProvenanceRejectsUnknownStatus(t *testing.T) {
	usage := map[string]any{
		"citations": []any{map[string]any{
			"id": "c1", "source_id": "s1", "status": "probably",
		}},
	}
	if _, err := cite.DecodeProvenance(usage); err == nil {
		t.Error("expected an error for an unknown citation status")
	}
}

/* ── live cross-runtime conformance ──────────────────────────────────────── */

func TestLiveV10EnvelopeAgainstCurrentRuntime(t *testing.T) {
	// The exact interop hal-go v0.1 could not do: send happi/1.0, parse
	// whatever version the runtime answers with.
	c := client.New("bash", referenceRuntimeFor(t), "run")
	env := envelope.Envelope{V: "happi/1.0", ID: "live-v10", Cmd: "version"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var types []string
	for ev := range stream {
		types = append(types, ev.EventType())
	}
	if strings.Join(types, ",") != "started,delta,completed" {
		t.Errorf("types = %v, want [started delta completed]", types)
	}
}

func TestLiveCiteVerifyCatchesFabricatedQuote(t *testing.T) {
	// A fabricated citation must resolve to not_found — arithmetic, not opinion.
	c := client.New("bash", referenceRuntimeFor(t), "run")
	env, err := cite.New("live-cv", []cite.Source{citeSource},
		[]cite.Citation{citeReal, citeFake}, false)
	if err != nil {
		t.Fatalf("cite.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := c.Dispatch(ctx, env)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	var completed *events.Completed
	for ev := range stream {
		if done, ok := ev.(*events.Completed); ok {
			completed = done
		}
	}
	if completed == nil {
		t.Fatal("no completed event in stream")
	}
	p, err := cite.DecodeProvenance(completed.Usage)
	if err != nil {
		t.Fatalf("DecodeProvenance: %v", err)
	}
	if len(p.NotFound()) != 1 || p.NotFound()[0].ID != "c2" {
		t.Errorf("NotFound = %v, want [c2]", p.NotFound())
	}
	if p.GroundingRate != 0.5 {
		t.Errorf("GroundingRate = %v, want 0.5", p.GroundingRate)
	}
}
