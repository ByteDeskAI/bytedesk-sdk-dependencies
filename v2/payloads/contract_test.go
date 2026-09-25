package payloads

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestChunkFitsWireAndBounds(t *testing.T) {
	v := AppendRequest{HandleID: "handle", Offset: MaxAssembledBytes - MaxChunkBytes, Data: base64.StdEncoding.EncodeToString(make([]byte, MaxChunkBytes))}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if len(raw) >= 64<<10 {
		t.Fatalf("wire size %d", len(raw))
	}
	v.Offset = ^uint64(0)
	if v.Validate() == nil {
		t.Fatal("overflow accepted")
	}
	v.Offset = 0
	v.Data = base64.StdEncoding.EncodeToString(make([]byte, MaxChunkBytes+1))
	if v.Validate() == nil {
		t.Fatal("large chunk accepted")
	}
	v.Data = "YQ"
	if v.Validate() == nil {
		t.Fatal("noncanonical base64 accepted")
	}
}
func TestCreateAndTextBounds(t *testing.T) {
	v := CreateRequest{ProviderID: "jev", Purpose: PurposeDecision, Size: MaxAssembledBytes, SHA256: strings.Repeat("a", 64), TTLSeconds: 600}
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	v.TTLSeconds = 601
	if v.Validate() == nil {
		t.Fatal("TTL accepted")
	}
	v.TTLSeconds = 600
	v.Size++
	if v.Validate() == nil {
		t.Fatal("large size accepted")
	}
	for _, v := range []TextInput{{}, {Inline: "hello", HandleID: "handle"}, {Inline: strings.Repeat("x", MaxInlineTextBytes+1)}} {
		if v.Validate() == nil {
			t.Fatal("invalid text source accepted")
		}
	}
}
