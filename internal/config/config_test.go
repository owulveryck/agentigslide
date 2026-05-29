package config

import (
	"os"
	"testing"
)

func TestTemplateDir(t *testing.T) {
	cfg := SlidesConfig{TemplateID: "abc123"}
	if got := cfg.TemplateDir(); got != "template/abc123" {
		t.Errorf("TemplateDir() = %q, want %q", got, "template/abc123")
	}
}

func TestEffectiveTemplateIndex_Default(t *testing.T) {
	cfg := SlidesConfig{TemplateID: "abc123"}
	want := "template/abc123/template_index.json"
	if got := cfg.EffectiveTemplateIndex(); got != want {
		t.Errorf("EffectiveTemplateIndex() = %q, want %q", got, want)
	}
}

func TestEffectiveTemplateIndex_Explicit(t *testing.T) {
	cfg := SlidesConfig{TemplateID: "abc123", TemplateIndex: "/custom/index.json"}
	if got := cfg.EffectiveTemplateIndex(); got != "/custom/index.json" {
		t.Errorf("EffectiveTemplateIndex() = %q, want %q", got, "/custom/index.json")
	}
}

func TestLoadSlidesConfig_RequiresTemplateID(t *testing.T) {
	os.Unsetenv("SLIDES_TEMPLATE_ID")
	_, err := LoadSlidesConfig()
	if err == nil {
		t.Fatal("expected error when SLIDES_TEMPLATE_ID is unset")
	}
}

func TestLoadSlidesConfig_Success(t *testing.T) {
	t.Setenv("SLIDES_TEMPLATE_ID", "test-id-42")
	t.Setenv("SLIDES_CREDENTIALS", "/tmp/creds.json")

	cfg, err := LoadSlidesConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TemplateID != "test-id-42" {
		t.Errorf("TemplateID = %q, want %q", cfg.TemplateID, "test-id-42")
	}
	if cfg.Credentials != "/tmp/creds.json" {
		t.Errorf("Credentials = %q, want %q", cfg.Credentials, "/tmp/creds.json")
	}
}
