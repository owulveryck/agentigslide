package outliner

import (
	"strings"
	"testing"
)

func TestDetectStructuredInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "35 slides with em-dash",
			input: `## SLIDE 1 — Couverture
Du chariot de Goldman à l'agent IA

## SLIDE 2 — 1937, Goldman
Le premier remède à l'abandon de panier

## SLIDE 3 — +40%
+40% volume d'achat

## SLIDE 4 — E-commerce
Naissance de l'e-commerce`,
			want: true,
		},
		{
			name: "slides with hyphen separator",
			input: `## SLIDE 1 - Intro
Content

## SLIDE 2 - Body
Content

## SLIDE 3 - End
Content`,
			want: true,
		},
		{
			name: "slides with colon separator",
			input: `## SLIDE 1: Intro
Content

## SLIDE 2: Body
Content

## SLIDE 3: End
Content`,
			want: true,
		},
		{
			name:  "freeform prose",
			input: "Fais-moi une présentation sur l'IA dans le commerce. Parle de l'historique, des tendances actuelles et des perspectives.",
			want:  false,
		},
		{
			name: "only 2 markers - below threshold",
			input: `## SLIDE 1 — Intro
Content

## SLIDE 2 — End
Content`,
			want: false,
		},
		{
			name: "regular markdown headings without SLIDE keyword",
			input: `## Introduction
Some content

## Section 1
More content

## Conclusion
Final content`,
			want: false,
		},
		{
			name: "case insensitive",
			input: `## slide 1 — Intro
Content

## Slide 2 — Body
Content

## SLIDE 3 — End
Content`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectStructuredInput(tt.input)
			if got != tt.want {
				t.Errorf("detectStructuredInput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildStructuredUserMessage(t *testing.T) {
	input := `## SLIDE 1 — Couverture
Du chariot de Goldman

## SLIDE 2 — Contenu
Le premier remède`

	msg := buildStructuredUserMessage(input)

	if !strings.Contains(msg, "DÉJÀ STRUCTURÉE") {
		t.Error("message should contain preservation instructions")
	}
	if !strings.Contains(msg, "Ne fusionne PAS") {
		t.Error("message should contain anti-merge instruction")
	}
	if !strings.Contains(msg, input) {
		t.Error("message should contain the original user request")
	}
}
