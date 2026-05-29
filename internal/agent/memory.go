package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/owulveryck/agentigslide/internal/vertex"
)

const synthesisPrompt = `Tu es un assistant qui analyse les erreurs détectées lors de la génération de présentations Google Slides par un pipeline multi-agent.

Tu reçois :
1. Le log des erreurs détectées et corrigées lors de la dernière exécution
2. Les guidelines existantes (mémoire) de chaque agent

Ton rôle : produire des guidelines mises à jour pour chaque agent concerné.

RÈGLES :
- Écris en français
- Chaque guideline doit être actionnable et spécifique (pas de généralités)
- Conserve les guidelines existantes qui restent pertinentes
- Ajoute de nouvelles guidelines basées sur les erreurs récentes
- Supprime les guidelines obsolètes ou contradictoires
- Ne répète pas les règles déjà présentes dans le prompt système de l'agent
- Limite-toi à 10-15 guidelines maximum par agent
- Format : liste à puces en Markdown

Réponds avec un JSON contenant les agents nécessitant des mises à jour :
{"updates": [{"agent": "nom_agent", "guidelines": "contenu markdown"}]}`

type synthesisInput struct {
	IssueLog         []IssueRecord     `json:"issueLog"`
	ExistingMemories map[string]string `json:"existingMemories"`
}

type synthesisOutput struct {
	Updates []struct {
		Agent      string `json:"agent"`
		Guidelines string `json:"guidelines"`
	} `json:"updates"`
}

// SynthesizeMemory calls a fast LLM to analyze the issue log and produce
// updated guidelines for each agent that had errors.
func SynthesizeMemory(ctx context.Context, client *vertex.Client, model string, issueLog IssueLog, existingMemories map[string]string) (map[string]string, error) {
	if !issueLog.HasIssues() {
		return nil, nil
	}

	input := synthesisInput{
		IssueLog:         issueLog,
		ExistingMemories: existingMemories,
	}

	inputJSON, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("memory synthesis: failed to marshal input: %w", err)
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("DONNÉES D'ENTRÉE :\n%s\n\nAnalyse ces erreurs et produis les guidelines mises à jour.", string(inputJSON)),
		}},
	}}

	responseText, err := client.RawPredict(ctx, model, messages,
		vertex.WithSystem(synthesisPrompt),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(8192),
	)
	if err != nil {
		return nil, fmt.Errorf("memory synthesis LLM call failed: %w", err)
	}

	jsonStr := extractJSON(responseText)

	var output synthesisOutput
	if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
		return nil, fmt.Errorf("memory synthesis: failed to parse response: %w (response: %s)", err, responseText)
	}

	proposals := make(map[string]string)
	for _, u := range output.Updates {
		if u.Agent != "" && u.Guidelines != "" {
			proposals[u.Agent] = strings.TrimSpace(u.Guidelines)
		}
	}

	return proposals, nil
}

// FormatMemoryProposals formats the proposed memory updates for display.
func FormatMemoryProposals(proposals map[string]string) string {
	if len(proposals) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== PROPOSITIONS DE GUIDELINES ===\n\n")
	for agent, guidelines := range proposals {
		sb.WriteString(fmt.Sprintf("--- %s ---\n", strings.ToUpper(agent)))
		sb.WriteString(guidelines)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// WriteMemoryFiles writes the approved memory guidelines to the template
// directory as {AGENT}_MEMORY.md files.
func WriteMemoryFiles(templateDir string, approved map[string]string) error {
	for agent, content := range approved {
		filename := strings.ToUpper(agent) + "_MEMORY.md"
		p := filepath.Join(templateDir, filename)
		if err := os.WriteFile(p, []byte(content+"\n"), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", p, err)
		}
		slog.Info("wrote agent memory", "agent", agent, "path", p)
	}
	return nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}
