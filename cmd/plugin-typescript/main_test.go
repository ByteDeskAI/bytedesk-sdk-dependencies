package main

import (
	"bytes"
	"os"
	"testing"
)

func TestBrowserContractsMatchCanonicalGoModel(t *testing.T) {
	want, err := os.ReadFile("../../typescript/contracts.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(declarations(), want) {
		t.Fatal("browser contracts drifted; run go run ./cmd/plugin-typescript -out typescript/contracts.d.ts")
	}
}
