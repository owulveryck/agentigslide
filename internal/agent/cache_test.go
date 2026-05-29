package agent

import (
	"strings"
	"testing"
)

func TestBuildSystemBlocks(t *testing.T) {
	t.Run("without template instructions returns single block", func(t *testing.T) {
		blocks := BuildSystemBlocks("system prompt here", "", "")
		if len(blocks) != 1 {
			t.Fatalf("expected 1 block, got %d", len(blocks))
		}
		if blocks[0].Text != "system prompt here" {
			t.Errorf("text = %q", blocks[0].Text)
		}
		if blocks[0].CacheControl == nil {
			t.Fatal("CacheControl should be set")
		}
		if blocks[0].CacheControl.Type != "ephemeral" {
			t.Errorf("CacheControl.Type = %q, want ephemeral", blocks[0].CacheControl.Type)
		}
	})

	t.Run("with template instructions returns two blocks", func(t *testing.T) {
		blocks := BuildSystemBlocks("system prompt", "template rules", "")
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		if blocks[0].Text != "system prompt" {
			t.Errorf("block 0 text = %q", blocks[0].Text)
		}
		if blocks[0].CacheControl != nil {
			t.Error("block 0 should not have CacheControl")
		}
		if blocks[1].CacheControl == nil {
			t.Fatal("block 1 should have CacheControl")
		}
		if blocks[1].CacheControl.Type != "ephemeral" {
			t.Errorf("block 1 CacheControl.Type = %q, want ephemeral", blocks[1].CacheControl.Type)
		}
	})

	t.Run("template block includes prefix", func(t *testing.T) {
		blocks := BuildSystemBlocks("sys", "my rules", "")
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		if blocks[1].Text != "INSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\nmy rules" {
			t.Errorf("block 1 text = %q", blocks[1].Text)
		}
	})

	t.Run("with agent memory returns three blocks", func(t *testing.T) {
		blocks := BuildSystemBlocks("sys", "rules", "memory content")
		if len(blocks) != 3 {
			t.Fatalf("expected 3 blocks, got %d", len(blocks))
		}
		if blocks[0].CacheControl != nil {
			t.Error("block 0 should not have CacheControl")
		}
		if blocks[1].CacheControl != nil {
			t.Error("block 1 (memory) should not have CacheControl")
		}
		if blocks[2].CacheControl == nil {
			t.Fatal("block 2 (last) should have CacheControl")
		}
		if !strings.Contains(blocks[1].Text, "MÉMOIRE DE L'AGENT") {
			t.Errorf("block 1 should contain memory prefix, got %q", blocks[1].Text)
		}
		if !strings.Contains(blocks[1].Text, "memory content") {
			t.Errorf("block 1 should contain memory content, got %q", blocks[1].Text)
		}
	})

	t.Run("with agent memory only returns two blocks", func(t *testing.T) {
		blocks := BuildSystemBlocks("sys", "", "memory content")
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		if blocks[1].CacheControl == nil {
			t.Fatal("last block should have CacheControl")
		}
	})
}
