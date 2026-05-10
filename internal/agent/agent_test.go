package agent

import (
	"testing"
)

func TestDefaultProvider(t *testing.T) {
	p := DefaultProvider()
	if p == nil {
		t.Fatal("DefaultProvider returned nil")
	}
	if p.Org != ProviderOrg {
		t.Errorf("Org = %q, want %q", p.Org, ProviderOrg)
	}
	if p.URL != ProviderURL {
		t.Errorf("URL = %q, want %q", p.URL, ProviderURL)
	}
}

func TestDefaultInputModes(t *testing.T) {
	modes := DefaultInputModes()
	if len(modes) != 2 {
		t.Fatalf("expected 2 input modes, got %d", len(modes))
	}
	if modes[0] != "application/json" {
		t.Errorf("modes[0] = %q, want application/json", modes[0])
	}
	if modes[1] != "text/plain" {
		t.Errorf("modes[1] = %q, want text/plain", modes[1])
	}
}

func TestDefaultOutputModes(t *testing.T) {
	modes := DefaultOutputModes()
	if len(modes) != 1 {
		t.Fatalf("expected 1 output mode, got %d", len(modes))
	}
	if modes[0] != "application/json" {
		t.Errorf("modes[0] = %q, want application/json", modes[0])
	}
}
