package templateindex

import (
	_ "embed"
	"sort"
	"strings"

	"github.com/owulveryck/agentigslide/internal/model"
)

// maxKeywords is the maximum number of keywords retained per slide.
const maxKeywords = 15

// minKeywordLength is the minimum character length for a word to be considered
// as a keyword. Shorter words are typically articles or prepositions.
const minKeywordLength = 3

// ExtractKeywords returns discriminative keywords from a slide's intention and
// description. It tokenizes the text, filters out stop words (loaded from the
// embedded stopwords_fr.txt file), and returns up to [maxKeywords] results
// sorted by length descending (longer words are more discriminative).
func ExtractKeywords(analysis model.SlideAnalysis) []string {
	keywords := make(map[string]bool)

	text := strings.ToLower(analysis.Intention + " " + analysis.Description)

	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.' || r == ':' || r == ';' ||
			r == '/' || r == '-' || r == '(' || r == ')' ||
			r == '\'' || r == '"' || r == '«' || r == '»'
	})

	for _, word := range words {
		word = strings.TrimSpace(word)
		if len(word) >= minKeywordLength && !stopWords[word] {
			keywords[word] = true
		}
	}

	result := make([]string, 0, len(keywords))
	for kw := range keywords {
		result = append(result, kw)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) != len(result[j]) {
			return len(result[i]) > len(result[j])
		}
		return result[i] < result[j]
	})

	if len(result) > maxKeywords {
		result = result[:maxKeywords]
	}

	return result
}

//go:embed stopwords_fr.txt
var stopWordsFile string

// stopWords is built at init time from the embedded stopwords_fr.txt file.
// Each non-empty, non-comment line becomes a stop word entry.
var stopWords = func() map[string]bool {
	m := make(map[string]bool)
	for _, line := range strings.Split(stopWordsFile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m[line] = true
	}
	return m
}()
