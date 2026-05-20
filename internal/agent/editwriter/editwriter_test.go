package editwriter

import (
	"encoding/json"
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestBuildEditWriterTool(t *testing.T) {
	t.Run("single modification", func(t *testing.T) {
		mods := []model.ModificationIntent{
			{VariableName: "obj-abc", Intention: "change the title"},
		}
		tool := buildEditWriterTool(mods)

		if tool.Name != "produce_modifications" {
			t.Errorf("name = %q, want %q", tool.Name, "produce_modifications")
		}

		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid JSON schema: %v", err)
		}
		props := schema["properties"].(map[string]any)
		if _, ok := props["obj-abc"]; !ok {
			t.Error("expected property obj-abc in schema")
		}
		req := schema["required"].([]any)
		if len(req) != 1 || req[0].(string) != "obj-abc" {
			t.Errorf("required = %v, want [obj-abc]", req)
		}
	})

	t.Run("multiple modifications", func(t *testing.T) {
		mods := []model.ModificationIntent{
			{VariableName: "obj-1", Intention: "intent 1"},
			{VariableName: "obj-2", Intention: "intent 2"},
			{VariableName: "obj-3", Intention: "intent 3"},
		}
		tool := buildEditWriterTool(mods)

		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid JSON schema: %v", err)
		}
		props := schema["properties"].(map[string]any)
		if len(props) != 3 {
			t.Errorf("expected 3 properties, got %d", len(props))
		}
		req := schema["required"].([]any)
		if len(req) != 3 {
			t.Errorf("expected 3 required fields, got %d", len(req))
		}
	})

	t.Run("description includes intention", func(t *testing.T) {
		mods := []model.ModificationIntent{
			{VariableName: "title-obj", Intention: "Update title to mention AI"},
		}
		tool := buildEditWriterTool(mods)

		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid JSON schema: %v", err)
		}
		props := schema["properties"].(map[string]any)
		prop := props["title-obj"].(map[string]any)
		desc := prop["description"].(string)
		if desc == "" {
			t.Error("description should not be empty")
		}
	})

	t.Run("schema is valid JSON object", func(t *testing.T) {
		mods := []model.ModificationIntent{
			{VariableName: "a", Intention: "test"},
		}
		tool := buildEditWriterTool(mods)
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("schema is not valid JSON: %v", err)
		}
		if schema["type"] != "object" {
			t.Errorf("schema type = %v, want object", schema["type"])
		}
	})
}
