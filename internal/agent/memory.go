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
// updated guidelines for each agent that had errors. The returned Usage is
// the token consumption of the synthesis call itself, so the cost of the
// learning loop is observable like any other agent call.
func SynthesizeMemory(ctx context.Context, client *vertex.Client, model string, issueLog IssueLog, existingMemories map[string]string) (map[string]string, vertex.Usage, error) {
	if !issueLog.HasIssues() {
		return nil, vertex.Usage{}, nil
	}

	input := synthesisInput{
		IssueLog:         issueLog,
		ExistingMemories: existingMemories,
	}

	inputJSON, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("memory synthesis: failed to marshal input: %w", err)
	}

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("DONNÉES D'ENTRÉE :\n%s\n\nAnalyse ces erreurs et produis les guidelines mises à jour.", string(inputJSON)),
		}},
	}}

	resp, err := client.RawPredictFull(ctx, model, messages,
		vertex.WithSystem(synthesisPrompt),
		vertex.WithTemperature(0.2),
		vertex.WithMaxTokens(8192),
	)
	if err != nil {
		return nil, vertex.Usage{}, fmt.Errorf("memory synthesis LLM call failed: %w", err)
	}

	var responseText strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			responseText.WriteString(block.Text)
		}
	}

	jsonStr := extractJSON(responseText.String())

	var output synthesisOutput
	if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
		return nil, resp.Usage, fmt.Errorf("memory synthesis: failed to parse response: %w (response: %s)", err, responseText.String())
	}

	proposals := make(map[string]string)
	for _, u := range output.Updates {
		if u.Agent != "" && u.Guidelines != "" {
			proposals[u.Agent] = strings.TrimSpace(u.Guidelines)
		}
	}

	return proposals, resp.Usage, nil
}

// IsAdditiveUpdate reports whether the proposed guidelines only add to the
// existing ones: every non-empty existing line is still present verbatim in
// the proposal. Deletions and rewrites of existing guidelines are NOT
// additive — they are the litigious cases that require human confirmation
// (a bad guideline that disappears silently is as dangerous as a bad one
// that appears).
func IsAdditiveUpdate(existing, proposed string) bool {
	if strings.TrimSpace(existing) == "" {
		return true
	}
	proposedLines := make(map[string]bool)
	for _, line := range strings.Split(proposed, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			proposedLines[l] = true
		}
	}
	for _, line := range strings.Split(existing, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if !proposedLines[l] {
			return false
		}
	}
	return true
}

// HasLitigiousIssues reports whether the issue log contains events that make
// any memory update for the given agent litigious: a sanitized selection
// means the run degraded silently, so guidelines derived from it deserve a
// human eye.
func (l IssueLog) HasLitigiousIssues(agentName string) bool {
	for _, rec := range l {
		if rec.Agent != agentName {
			continue
		}
		for _, issue := range rec.Issues {
			if issue.IssueType == "sanitized_selection" {
				return true
			}
		}
	}
	return false
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
