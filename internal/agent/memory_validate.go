package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// GroundTruth holds the verifiable facts a learned memory rule may reference:
// the slide numbers that actually exist in the template index, and the slide
// numbers declared by the deck configuration (ADR 028). Any learned rule that
// contradicts these facts is rejected — configuration and code outrank
// learned memory.
type GroundTruth struct {
	CatalogSlides map[int]bool
	ConfigSlides  map[int]bool
}

// BuildGroundTruth derives the ground truth from the loaded template index
// and the configured deck invariants.
func BuildGroundTruth(index *model.TemplateIndex, inv DeckInvariants) GroundTruth {
	gt := GroundTruth{
		CatalogSlides: make(map[int]bool),
		ConfigSlides:  make(map[int]bool),
	}
	if index != nil {
		for _, s := range index.Slides {
			gt.CatalogSlides[s.SlideNumber] = true
		}
	}
	for _, n := range []int{inv.CoverSlide, inv.ClosingSlide, inv.SummarySlide} {
		if n > 0 {
			gt.ConfigSlides[n] = true
		}
	}
	return gt
}

// knownSlide reports whether a slide number exists in the catalog or the
// deck configuration.
func (gt GroundTruth) knownSlide(n int) bool {
	return gt.CatalogSlides[n] || gt.ConfigSlides[n]
}

// RejectedRule is a memory guideline line discarded by validation, with the
// reason. Rejected rules are quarantined for audit, never injected into
// prompts.
type RejectedRule struct {
	Agent  string
	Line   string
	Reason string
}

var memorySlideNumRe = regexp.MustCompile(`(?i)slide\s+(\d+)`)

// structuralAssertionMarkers are the phrases that make a memory line a deck
// structure rule. Structural invariants belong to the template configuration
// (COVER_SLIDE/CLOSING_SLIDE, ADR 029), never to learned memory: a learned
// structural belief can drift, self-reinforce, and fight the configuration —
// exactly what happened with the poisoned "SLIDE 325 est un fantôme" /
// "SLIDE 250 en conclusion" rules of edito-trace-v3.
var structuralAssertionMarkers = []string{
	"fantôme", "fantome", "inexistant", "n'existe pas", "n'existent pas",
	"hors catalogue", "premier slide", "dernier slide", "en tout premier",
	"en dernier", "couverture officielle", "conclusion officielle",
	"index 0", "index final",
}

// ValidateMemory filters a proposed (or existing) memory document for one
// agent against the ground truth (ADR 028). It works line by line, matching
// the bullet-list structure of memory files:
//   - a line referencing a slide number absent from catalog ∪ config is an
//     invented fact → rejected;
//   - a line referencing a slide number AND making a structural assertion is
//     a deck invariant in disguise → rejected (invariants live in config).
//
// Lines the validation cannot falsify are kept verbatim.
func ValidateMemory(agentName, proposed string, gt GroundTruth) (clean string, rejected []RejectedRule) {
	if strings.TrimSpace(proposed) == "" {
		return proposed, nil
	}
	var kept []string
	for _, line := range strings.Split(proposed, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			kept = append(kept, line)
			continue
		}

		matches := memorySlideNumRe.FindAllStringSubmatch(trimmed, -1)
		if len(matches) == 0 {
			kept = append(kept, line)
			continue
		}

		var unknown []int
		for _, m := range matches {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if !gt.knownSlide(n) {
				unknown = append(unknown, n)
			}
		}
		if len(unknown) > 0 {
			rejected = append(rejected, RejectedRule{
				Agent:  agentName,
				Line:   trimmed,
				Reason: fmt.Sprintf("references slide(s) %v absent from catalog and configuration (invented fact)", unknown),
			})
			continue
		}

		lower := strings.ToLower(trimmed)
		structural := ""
		for _, marker := range structuralAssertionMarkers {
			if strings.Contains(lower, marker) {
				structural = marker
				break
			}
		}
		if structural != "" {
			rejected = append(rejected, RejectedRule{
				Agent:  agentName,
				Line:   trimmed,
				Reason: fmt.Sprintf("structural assertion (%q) about specific slides — deck invariants belong to the template configuration (ADR 029), not learned memory", structural),
			})
			continue
		}

		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), rejected
}

// ValidateMemories applies ValidateMemory to every agent's memory document.
// Returns the cleaned map (entries that end up empty are removed) and all
// rejected rules.
func ValidateMemories(memories map[string]string, gt GroundTruth) (map[string]string, []RejectedRule) {
	clean := make(map[string]string, len(memories))
	var rejected []RejectedRule
	for agentName, content := range memories {
		c, rej := ValidateMemory(agentName, content, gt)
		rejected = append(rejected, rej...)
		if strings.TrimSpace(c) != "" {
			clean[agentName] = c
		}
	}
	return clean, rejected
}
