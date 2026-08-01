// Package happi holds the protocol version vocabulary shared by every
// hal-go package.
//
// It exists so the envelope and events packages agree on one accept-set
// rather than each carrying its own copy — two lists that must never drift
// apart are one list.
//
// # Emitting versus accepting
//
// A HAPPI runtime stamps events with ITS OWN version, never the version on
// the envelope it was handed. A happi/1.0 request dispatched to a happi/1.3
// runtime therefore comes back as happi/1.3 events. A client that compares
// the wire version for exact equality against a single constant can only
// ever talk to one runtime build; it will reject a newer runtime outright.
//
// So: emit Version, accept anything in AcceptedVersions, reject the rest.
package happi

// Version is the protocol version this library stamps on values it builds.
const Version = "happi/1.3"

// AcceptedVersions lists every protocol version this library accepts on the
// wire, oldest first. Full back-compat is a HAPPI guarantee: a v1.3 runtime
// accepts v1.0 envelopes, so a v1.0 client must tolerate v1.3 events.
var AcceptedVersions = []string{
	"happi/1.0",
	"happi/1.1",
	"happi/1.2",
	"happi/1.3",
}

// acceptedSet is derived from AcceptedVersions so the two cannot diverge.
var acceptedSet = newAcceptedSet()

func newAcceptedSet() map[string]struct{} {
	set := make(map[string]struct{}, len(AcceptedVersions))
	for _, v := range AcceptedVersions {
		set[v] = struct{}{}
	}
	return set
}

// IsAccepted reports whether v is a protocol version this library accepts.
func IsAccepted(v string) bool {
	_, ok := acceptedSet[v]
	return ok
}

// AcceptedList renders AcceptedVersions for error messages.
func AcceptedList() string {
	out := ""
	for i, v := range AcceptedVersions {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
