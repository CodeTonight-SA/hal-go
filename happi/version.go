// Package happi holds the protocol version vocabulary shared by every
// hal-go package.
//
// It exists so the envelope and events packages agree on one acceptance
// policy rather than each carrying its own copy — two rules that must never
// drift apart are one rule.
//
// # Emitting versus accepting
//
// A HAPPI runtime stamps events with ITS OWN version, never the version on
// the envelope it was handed. A happi/1.0 request dispatched to a happi/1.3
// runtime therefore comes back as happi/1.3 events. A client that compares
// the wire version for exact equality against a single constant can only
// ever talk to one runtime build; it will reject a newer runtime outright.
//
// # Why the policy is a MAJOR-version rule, not a fixed list
//
// The first fix for that bug was a hardcoded list {1.0, 1.1, 1.2, 1.3}. That
// merely deferred it: the day a runtime ships happi/1.4, every client
// rejects it again, for the same reason, and every SDK needs another release.
//
// HAPPI's compatibility promise is per MAJOR version — within v1, changes are
// additive and unknown fields are forwarded unchanged. So the honest policy
// is to accept ANY happi/1.x and reject a different major, where breaking
// changes are actually allowed to live. That makes forward compatibility
// structural instead of a maintenance chore.
//
// This is deliberately NOT "accept anything". happi/2.0 is refused, because
// a major bump is precisely the signal that assumptions no longer hold.
package happi

import (
	"strconv"
	"strings"
)

// Version is the protocol version this library stamps on values it builds.
const Version = "happi/1.3"

// Prefix is the scheme every HAPPI version string carries.
const Prefix = "happi/"

// SupportedMajor is the protocol major version this library implements.
// Any happi/<SupportedMajor>.<minor> is accepted, however new the minor.
const SupportedMajor = 1

// KnownVersions are the minors that existed when this library was written.
// Informational only — acceptance is decided by IsAccepted, not this list, so
// a newer minor is not rejected merely for being absent here.
var KnownVersions = []string{
	"happi/1.0",
	"happi/1.1",
	"happi/1.2",
	"happi/1.3",
}

// IsAccepted reports whether v is a protocol version this library accepts:
// any well-formed happi/<SupportedMajor>.<minor>.
//
// Rejected: a different major, a missing or non-numeric component, a negative
// minor, and anything not carrying the happi/ prefix.
func IsAccepted(v string) bool {
	major, minor, ok := parse(v)
	return ok && major == SupportedMajor && minor >= 0
}

// parse splits "happi/<major>.<minor>" into its numeric parts.
func parse(v string) (major, minor int, ok bool) {
	if !strings.HasPrefix(v, Prefix) {
		return 0, 0, false
	}
	rest := strings.TrimPrefix(v, Prefix)
	majorStr, minorStr, found := strings.Cut(rest, ".")
	if !found {
		return 0, 0, false
	}
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(minorStr)
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// AcceptedList renders the acceptance policy for error messages. It states
// the rule rather than enumerating a list, because the rule is what a reader
// needs in order to understand why their version was refused.
func AcceptedList() string {
	return Prefix + strconv.Itoa(SupportedMajor) + ".x"
}
