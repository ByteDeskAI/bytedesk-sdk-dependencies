package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"slices"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// GrantsDigestAlgorithm names the canonicalisation below. It is hashed first,
// so a future change to the encoding produces a different digest for the same
// manifest rather than a silent collision with records written by the old one.
const GrantsDigestAlgorithm = "bytedesk.plugin.grants.v3"

// GrantsDigest is the stable fingerprint of everything an operator consents to
// when they approve this plugin: permissions, serves, streams, kv, objects,
// needs, and side-effect capabilities. `plugin-sdk pack` prints it; a
// dev-grants entry is keyed by it, so a change to any of those fields re-asks
// the operator rather than silently widening what they already approved.
//
// It succeeds the gateway's manifestPermissionsDigest
// (src/kernel_host_grants.go:221), which covered only the three permission
// lists. That was complete while permissions were the whole of what a plugin
// could ask for. A v2 manifest also asks for exported services, provisioned
// storage and substrate capabilities, and a digest that ignores those would let
// a plugin add a 10 GiB stream to an approval the operator already gave.
//
// Two properties, both tested:
//
//   - Order-independent. Every list is sorted before hashing, so reformatting a
//     manifest does not invalidate consent and annoy an operator into approving
//     without reading.
//   - Change-sensitive. Every covered field is encoded, length-delimited, so no
//     concatenation of two values can collide with a third.
func GrantsDigest(m Manifest) string {
	h := sha256.New()
	write(h, GrantsDigestAlgorithm)

	section(h, "publish", patternStrings(m.Permissions.Publish))
	section(h, "request", patternStrings(m.Permissions.Request))
	section(h, "subscribe", patternStrings(m.Permissions.Subscribe))

	serves := make([]string, 0, len(m.Serves))
	for _, svc := range m.Serves {
		endpoints := make([]string, 0, len(svc.Endpoints))
		for _, ep := range svc.Endpoints {
			endpoints = append(endpoints, join(ep.Name, string(ep.Subject), ep.Point))
		}
		slices.Sort(endpoints)
		serves = append(serves, join(svc.Name, svc.Version, svc.QueueGroup, strings.Join(endpoints, "\x1e")))
	}
	section(h, "serves", serves)

	streams := make([]string, 0, len(m.Streams))
	for _, s := range m.Streams {
		subjects := patternStrings(s.Subjects)
		slices.Sort(subjects)
		streams = append(streams, join(s.Name,
			fmt.Sprint(s.MaxBytes), fmt.Sprint(s.MaxAgeSeconds), fmt.Sprint(s.MaxMsgs),
			strings.Join(subjects, "\x1e")))
	}
	section(h, "streams", streams)

	kv := make([]string, 0, len(m.KV))
	for _, b := range m.KV {
		kv = append(kv, join(b.Name, fmt.Sprint(b.MaxBytes), fmt.Sprint(b.TTLSeconds), fmt.Sprint(b.History)))
	}
	section(h, "kv", kv)

	objects := make([]string, 0, len(m.Objects))
	for _, b := range m.Objects {
		objects = append(objects, join(b.Name, fmt.Sprint(b.MaxBytes), fmt.Sprint(b.TTLSeconds)))
	}
	section(h, "objects", objects)

	section(h, "needs", slices.Clone(m.Needs))
	section(h, "capabilities", slices.Clone(m.Capabilities))

	return hex.EncodeToString(h.Sum(nil))
}

// section hashes one labelled, sorted list. The label and the count are hashed
// too, so moving an entry from one section to another moves the digest.
func section(h hash.Hash, label string, values []string) {
	slices.Sort(values)
	write(h, label)
	write(h, fmt.Sprint(len(values)))
	for _, v := range values {
		write(h, v)
	}
}

// write is the one length-delimited primitive: "<len>:<value>:". Without the
// length, "ab"+"c" and "a"+"bc" hash alike.
func write(h hash.Hash, s string) { fmt.Fprintf(h, "%d:%s:", len(s), s) }

// join encodes the fields of one record. The separator is a unit separator,
// which the subject grammar and every id rule already exclude.
func join(fields ...string) string { return strings.Join(fields, "\x1f") }

func patternStrings(in []bus.Pattern) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = string(p)
	}
	return out
}
