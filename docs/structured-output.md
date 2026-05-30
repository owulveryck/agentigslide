# Structured output via tool_use

Chaque agent du pipeline produit une sortie structurée en forçant Claude à appeler un outil dont le schema JSON définit le format attendu. Ce pattern est utilisé systématiquement pour garantir des sorties parsables et validables.

## Pattern

Trois étapes, identiques pour chaque agent :

### 1. Définition du tool avec JSON schema

Chaque agent définit un outil avec un `InputSchema` strict. Exemple simplifié pour l'Outliner ([`internal/agent/outliner/outliner.go`](../internal/agent/outliner/outliner.go)) :

```go
func (a *Agent) outlinerTool() vertex.Tool {
    return vertex.Tool{
        Name:        "produce_outline",
        Description: "Produce the structured outline...",
        InputSchema: json.RawMessage(`{
            "type": "object",
            "properties": {
                "presentationTitle": {"type": "string"},
                "sections": {
                    "type": "array",
                    "items": {
                        "properties": {
                            "title": {"type": "string"},
                            "slideNeeds": { ... }
                        },
                        "required": ["title", "slideNeeds"]
                    }
                }
            },
            "required": ["presentationTitle", "sections"]
        }`),
    }
}
```

### 2. Appel API avec `tool_choice` forcé

```go
resp, err := a.client.RawPredictFull(ctx, a.model, messages,
    vertex.WithTools([]vertex.Tool{tool}),
    vertex.WithToolChoice(map[string]any{
        "type": "tool",
        "name": "produce_outline",
    }),
)
```

`tool_choice` avec `type: "tool"` force Claude à appeler l'outil spécifié plutôt que de répondre en texte libre. La sortie est donc toujours du JSON conforme au schema.

### 3. Extraction et parsing

```go
block := resp.ToolUseBlock()
if block == nil {
    return nil, fmt.Errorf("no tool_use block in response")
}

var outline agent.PresentationOutline
err := json.Unmarshal(block.Input, &outline)
```

`ToolUseBlock()` extrait le premier bloc de type `tool_use` de la réponse. `block.Input` contient le JSON des paramètres de l'outil, directement désérialisable dans la struct Go cible.

## Outils par agent

| Agent | Nom de l'outil | Sortie |
|-------|---------------|--------|
| Outliner | `produce_outline` | `PresentationOutline` |
| Selector | `select_templates` | `SelectionPlan` |
| Writer | `produce_slide_content` | `SlideContent` (schema dynamique selon les champs du template) |
| Designer | `design_diagram` | `DiagramSpec` |
| Reviewer | `submit_review` | `ReviewResult` |

Le Writer est un cas particulier : son schema est construit dynamiquement par `BuildWriterTool(fields)` en fonction des champs éditables du template sélectionné.

## Voir aussi

- [Validation](./validation.md) — validation programmatique après le parsing
- [Prompt caching](./prompt-caching.md) — optimisation des appels API
