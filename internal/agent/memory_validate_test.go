package agent

import (
	"strings"
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func testGroundTruth() GroundTruth {
	index := &model.TemplateIndex{Slides: []model.TemplateSlide{
		{SlideNumber: 1}, {SlideNumber: 15}, {SlideNumber: 35}, {SlideNumber: 325},
	}}
	return BuildGroundTruth(index, DeckInvariants{CoverSlide: 1, ClosingSlide: 325})
}

func TestValidateMemory_PoisonedRulesFromTraceV3(t *testing.T) {
	gt := testGroundTruth()

	// Actual poisoned rules found in REVIEWER_MEMORY.md after the v3 run:
	// SLIDE 250 does not exist in this catalog, and the structural assertions
	// contradict the configured invariants (CLOSING_SLIDE=325).
	poisoned := strings.Join([]string{
		"- **STRUCTURE OBLIGATOIRE : Exiger l'ajout d'un SLIDE 1 (couverture officielle) à l'index 0 en tout premier.**",
		"- **Vérifier EXPLICITEMENT que le SLIDE 250 (« THERE IS A BETTER WAY. ») est présent comme dernier slide.**",
		"- **Détecter et signaler les slides fantômes ou inexistants (ex. : SLIDE 325 si le catalogue ne va que jusqu'au SLIDE 250).**",
		"- Mesurer la longueur en caractères de chaque texte et la comparer à la capacité documentée du champ cible.",
		"- Rejeter immédiatement tout texte avec phrase suspendue ou syntagme nominal orphelin.",
	}, "\n")

	clean, rejected := ValidateMemory("reviewer", poisoned, gt)

	if len(rejected) != 3 {
		t.Fatalf("expected 3 rejected rules, got %d: %+v", len(rejected), rejected)
	}
	if strings.Contains(clean, "SLIDE 250") || strings.Contains(clean, "fantôme") || strings.Contains(clean, "SLIDE 1") {
		t.Errorf("poisoned rules survived validation:\n%s", clean)
	}
	// Legitimate editorial guidelines must survive.
	if !strings.Contains(clean, "phrase suspendue") || !strings.Contains(clean, "capacité documentée") {
		t.Errorf("legitimate rules were lost:\n%s", clean)
	}
}

func TestValidateMemory_KnownSlideStructuralAssertionRejected(t *testing.T) {
	gt := testGroundTruth()
	// SLIDE 325 exists, but "dernier slide" is a structural invariant that
	// belongs to CLOSING_SLIDE configuration, not learned memory.
	rule := "- Vérifier que le SLIDE 325 est bien le dernier slide de la présentation."
	clean, rejected := ValidateMemory("reviewer", rule, gt)
	if len(rejected) != 1 {
		t.Fatalf("expected structural assertion rejected, got clean=%q rejected=%+v", clean, rejected)
	}
}

func TestValidateMemory_FactualReferenceToKnownSlideKept(t *testing.T) {
	gt := testGroundTruth()
	// A non-structural, factual note about an existing slide is kept: this is
	// the kind of knowledge that may legitimately accumulate (until it moves
	// to structured caveats).
	rule := "- Le SLIDE 35 convient bien aux présentations institutionnelles."
	clean, rejected := ValidateMemory("writer", rule, gt)
	if len(rejected) != 0 {
		t.Fatalf("factual rule about an existing slide should be kept, rejected=%+v", rejected)
	}
	if !strings.Contains(clean, "SLIDE 35") {
		t.Errorf("rule lost: %q", clean)
	}
}

func TestValidateMemory_NoSlideNumbersUntouched(t *testing.T) {
	gt := testGroundTruth()
	content := "- Toujours écrire des phrases complètes.\n- Éviter les listes à puces dans les titres."
	clean, rejected := ValidateMemory("writer", content, gt)
	if len(rejected) != 0 || clean != content {
		t.Errorf("content without slide numbers must pass verbatim, got clean=%q rejected=%+v", clean, rejected)
	}
}

func TestValidateMemories_EmptyAfterCleaningRemoved(t *testing.T) {
	gt := testGroundTruth()
	memories := map[string]string{
		"reviewer": "- SLIDE 999 est interdit.",
		"writer":   "- Phrases courtes.",
	}
	clean, rejected := ValidateMemories(memories, gt)
	if len(rejected) != 1 {
		t.Fatalf("expected 1 rejection, got %+v", rejected)
	}
	if _, ok := clean["reviewer"]; ok {
		t.Error("reviewer memory should be removed when empty after cleaning")
	}
	if clean["writer"] != "- Phrases courtes." {
		t.Errorf("writer memory altered: %q", clean["writer"])
	}
}
