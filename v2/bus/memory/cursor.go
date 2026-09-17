package memory

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/ByteDeskAI/bytedesk-sdk-dependencies/v2/bus"
)

// Cursors are opaque and unforgeable.
//
// A cursor is "<stream>|<consumer>|<seq>" signed with HMAC-SHA256 under a key
// created with the Store and kept across Restart. The holder can store it and
// hand it back; it cannot fabricate one, and a tampered cursor is refused
// rather than interpreted, so a cursor is never a way to read a position the
// holder was not given.

func (s *Store) mintCursor(stream, consumer string, seq bus.Seq) bus.Cursor {
	body := cursorBody(stream, consumer, seq)
	mac := s.signCursor(body)
	token := base64.RawURLEncoding.EncodeToString([]byte(body)) + "." + base64.RawURLEncoding.EncodeToString(mac)
	return bus.NewCursor(token)
}

// readCursor verifies c and returns the sequence it covers. A cursor for a
// different stream, or one whose signature does not verify, is refused.
func (s *Store) readCursor(c bus.Cursor, stream string) (bus.Seq, error) {
	refuse := func(why string) (bus.Seq, error) {
		return 0, bus.Fault{Code: bus.FaultDenied, Op: "cursor:" + stream, Message: "cursor refused: " + why}
	}
	if c.IsZero() {
		return refuse("empty")
	}
	raw, sig, ok := strings.Cut(c.Token(), ".")
	if !ok {
		return refuse("malformed")
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return refuse("malformed")
	}
	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return refuse("malformed")
	}
	if !hmac.Equal(mac, s.signCursor(string(body))) {
		return refuse("signature does not verify")
	}
	parts := strings.Split(string(body), "|")
	if len(parts) != 3 {
		return refuse("malformed")
	}
	if parts[0] != stream {
		return refuse("belongs to stream " + strconv.Quote(parts[0]))
	}
	seq, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return refuse("malformed")
	}
	return bus.Seq(seq), nil
}

func (s *Store) signCursor(body string) []byte {
	mac := hmac.New(sha256.New, s.cursorKey)
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

func cursorBody(stream, consumer string, seq bus.Seq) string {
	return stream + "|" + consumer + "|" + strconv.FormatUint(uint64(seq), 10)
}
