package plugin

import (
	"encoding/json"
	"testing"
)

func TestSealedZoneRefusesEveryImplementer(t *testing.T) {
	point := ExtensionPoint{Name: "acme.zone.panel", Zone: "panel", Sealed: true, RequiredBase: "acme.Widget"}
	err := AdmitImplementer(point, Provider{Point: point.Name, ID: "one", Base: "acme.Widget"})
	if err == nil {
		t.Fatal("sealed zone admitted an implementer")
	}
}

func TestOpenZoneAcceptsOnlyTheRequiredBase(t *testing.T) {
	point := ExtensionPoint{Name: "acme.zone.panel", Zone: "panel", Interface: "acme.Widget"}
	if err := AdmitImplementer(point, Provider{Point: point.Name, ID: "ok", Base: "acme.Widget"}); err != nil {
		t.Fatal(err)
	}
	if err := AdmitImplementer(point, Provider{Point: point.Name, ID: "other", Base: "acme.Other"}); err == nil {
		t.Fatal("open zone admitted a different base")
	}
	if err := AdmitImplementer(point, Provider{Point: point.Name, ID: "missing"}); err == nil {
		t.Fatal("open zone admitted a provider that names no base")
	}
	// Zero implementers is the caller's success: the point itself is valid.
	if err := (Manifest{ID: "owner", Version: "1", Publisher: &Publisher{ID: "acme", Name: "Acme"}, Extends: []ExtensionPoint{point}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredBaseFieldWinsOverInterface(t *testing.T) {
	point := ExtensionPoint{Name: "acme.zone.panel", Zone: "panel", Interface: "old.v1", RequiredBase: "acme.Widget"}
	if point.BaseID() != "acme.Widget" {
		t.Fatalf("base = %q", point.BaseID())
	}
	if err := AdmitImplementer(point, Provider{Point: point.Name, ID: "ok", Base: "acme.Widget"}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalPointStillAdmitsAProviderWithoutABase(t *testing.T) {
	point := ExtensionPoint{Name: "acme.widgets.panel", Interface: "x.v1"}
	if err := AdmitImplementer(point, Provider{Point: point.Name, ID: "legacy"}); err != nil {
		t.Fatal(err)
	}
}

func TestAssignPublisherColorsReplacesACollision(t *testing.T) {
	palette := []string{"#a", "#b", "#c"}
	got := AssignPublisherColors([]Publisher{
		{ID: "beta", Name: "Beta", Color: "#a"},
		{ID: "alpha", Name: "Alpha", Color: "#a"},
		{ID: "gamma", Name: "Gamma", Color: "not-in-palette"},
	}, palette)
	if got["alpha"] != "#a" {
		t.Fatalf("first publisher kept hint: %+v", got)
	}
	if got["beta"] != "#b" {
		t.Fatalf("collision was not reassigned: %+v", got)
	}
	if got["gamma"] != "#c" {
		t.Fatalf("unknown hint was not reassigned: %+v", got)
	}
	if got["alpha"] == got["beta"] || got["beta"] == got["gamma"] {
		t.Fatalf("colors collided: %+v", got)
	}
}

func TestZoneFieldsRoundTripJSON(t *testing.T) {
	raw := []byte(`{"id":"example","version":"1","publisher":{"id":"acme","name":"Acme","color":"#a"},"extends":[{"name":"acme.zone.panel","zone":"panel","sealed":true,"requiredBase":"acme.Widget"}],"implements":[{"point":"acme.zone.panel","id":"one","base":"acme.Widget"}]}`)
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Publisher == nil || m.Publisher.Color != "#a" {
		t.Fatalf("publisher = %+v", m.Publisher)
	}
	if len(m.Extends) != 1 || m.Extends[0].Zone != "panel" || !m.Extends[0].Sealed || m.Extends[0].RequiredBase != "acme.Widget" {
		t.Fatalf("extends = %+v", m.Extends)
	}
	if len(m.Implements) != 1 || m.Implements[0].Base != "acme.Widget" {
		t.Fatalf("implements = %+v", m.Implements)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatal("marshal")
	}
}
