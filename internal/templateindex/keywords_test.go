package templateindex

import (
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestExtractKeywords_FiltersStopWords(t *testing.T) {
	analysis := model.SlideAnalysis{
		Intention:   "Présentation de la société",
		Description: "Ce slide contient un titre principal et des éléments visuels réutilisables",
	}
	keywords := ExtractKeywords(analysis)
	for _, kw := range keywords {
		if stopWords[kw] {
			t.Errorf("stop word %q should have been filtered", kw)
		}
	}
}

func TestExtractKeywords_LimitTo15(t *testing.T) {
	// Build a description with many unique long words to exceed the limit.
	analysis := model.SlideAnalysis{
		Intention: "architecture microservices containerisation orchestration",
		Description: "infrastructure monitoring observabilité déploiement " +
			"scalabilité performance optimisation sécurisation " +
			"automatisation intégration modernisation transformation " +
			"industrialisation standardisation virtualisation",
	}
	keywords := ExtractKeywords(analysis)
	if len(keywords) > maxKeywords {
		t.Errorf("got %d keywords, want at most %d", len(keywords), maxKeywords)
	}
}

func TestExtractKeywords_SortedByLengthDesc(t *testing.T) {
	analysis := model.SlideAnalysis{
		Intention:   "innovation cloud architecture",
		Description: "modernisation des systèmes legacy",
	}
	keywords := ExtractKeywords(analysis)
	for i := 1; i < len(keywords); i++ {
		if len(keywords[i]) > len(keywords[i-1]) {
			t.Errorf("keywords not sorted by length: %q (len %d) after %q (len %d)",
				keywords[i], len(keywords[i]), keywords[i-1], len(keywords[i-1]))
		}
	}
}

func TestExtractKeywords_MinLength(t *testing.T) {
	analysis := model.SlideAnalysis{
		Intention:   "AI ML DL IA",
		Description: "un ab cd ef",
	}
	keywords := ExtractKeywords(analysis)
	for _, kw := range keywords {
		if len(kw) < minKeywordLength {
			t.Errorf("keyword %q is shorter than minimum length %d", kw, minKeywordLength)
		}
	}
}

func TestExtractKeywords_EmptyInput(t *testing.T) {
	analysis := model.SlideAnalysis{}
	keywords := ExtractKeywords(analysis)
	if len(keywords) != 0 {
		t.Errorf("expected no keywords for empty input, got %v", keywords)
	}
}

func TestStopWordsLoaded(t *testing.T) {
	if len(stopWords) == 0 {
		t.Fatal("stopWords map is empty — embedded file not loaded correctly")
	}
	expected := []string{"de", "la", "lorem", "ipsum", "octo", "template"}
	for _, w := range expected {
		if !stopWords[w] {
			t.Errorf("expected %q to be a stop word", w)
		}
	}
}
