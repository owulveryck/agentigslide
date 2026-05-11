package writer

import (
	"encoding/json"
	"testing"

	"github.com/owulveryck/agentigslide/internal/agent"
)

func TestBuildWriterTool(t *testing.T) {
	t.Run("field without maxChars has no maxLength", func(t *testing.T) {
		fields := []agent.TemplateField{{VariableName: "titleShape", Role: "titre_principal", MaxChars: 0}}
		tool := BuildWriterTool(fields)

		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid JSON schema: %v", err)
		}
		props := schema["properties"].(map[string]any)
		titleProp := props["titleShape"].(map[string]any)
		if _, ok := titleProp["maxLength"]; ok {
			t.Error("field with MaxChars=0 should not have maxLength")
		}
	})

	t.Run("field with maxChars applies 90% limit", func(t *testing.T) {
		fields := []agent.TemplateField{{VariableName: "bodyShape", Role: "contenu", MaxChars: 100}}
		tool := BuildWriterTool(fields)

		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid JSON schema: %v", err)
		}
		props := schema["properties"].(map[string]any)
		bodyProp := props["bodyShape"].(map[string]any)
		ml, ok := bodyProp["maxLength"]
		if !ok {
			t.Fatal("expected maxLength for field with MaxChars>0")
		}
		if int(ml.(float64)) != 90 {
			t.Errorf("maxLength = %v, want 90 (90%% of 100)", ml)
		}
	})

	t.Run("required fields are sorted alphabetically", func(t *testing.T) {
		fields := []agent.TemplateField{
			{VariableName: "charlie", Role: "contenu"},
			{VariableName: "alpha", Role: "titre"},
			{VariableName: "bravo", Role: "sous-titre"},
		}
		tool := BuildWriterTool(fields)

		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("invalid JSON schema: %v", err)
		}
		req := schema["required"].([]any)
		if len(req) != 3 {
			t.Fatalf("expected 3 required, got %d", len(req))
		}
		want := []string{"alpha", "bravo", "charlie"}
		for i, r := range req {
			if r.(string) != want[i] {
				t.Errorf("required[%d] = %q, want %q", i, r, want[i])
			}
		}
	})

	t.Run("schema is valid JSON", func(t *testing.T) {
		fields := []agent.TemplateField{
			{VariableName: "a", Role: "titre", MaxChars: 50},
			{VariableName: "b", Role: "contenu"},
		}
		tool := BuildWriterTool(fields)
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Fatalf("schema is not valid JSON: %v", err)
		}
		if schema["type"] != "object" {
			t.Errorf("schema type = %v, want object", schema["type"])
		}
	})

	t.Run("tool name and description", func(t *testing.T) {
		fields := []agent.TemplateField{{VariableName: "x", Role: "test"}}
		tool := BuildWriterTool(fields)
		if tool.Name != "produce_slide_content" {
			t.Errorf("name = %q, want %q", tool.Name, "produce_slide_content")
		}
		if tool.Description == "" {
			t.Error("description should not be empty")
		}
	})
}
