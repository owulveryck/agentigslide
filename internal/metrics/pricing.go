package metrics

import "strings"

// ModelPricing holds per-million-token prices for a model family.
type ModelPricing struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheReadPerMTok  float64
	CacheWritePerMTok float64
}

var defaultPricing = []struct {
	keyword string
	pricing ModelPricing
}{
	{"opus", ModelPricing{InputPerMTok: 15.0, OutputPerMTok: 75.0, CacheReadPerMTok: 1.50, CacheWritePerMTok: 18.75}},
	{"sonnet", ModelPricing{InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75}},
	{"haiku", ModelPricing{InputPerMTok: 0.80, OutputPerMTok: 4.0, CacheReadPerMTok: 0.08, CacheWritePerMTok: 1.0}},
}

// LookupPricing returns the pricing for a model name by matching known
// keywords (opus, sonnet, haiku). Returns zero pricing if no match.
func LookupPricing(model string) ModelPricing {
	lower := strings.ToLower(model)
	for _, entry := range defaultPricing {
		if strings.Contains(lower, entry.keyword) {
			return entry.pricing
		}
	}
	return ModelPricing{}
}

// EstimateRowCost computes the estimated cost in USD for an AgentRow.
func EstimateRowCost(row *AgentRow) float64 {
	p := LookupPricing(row.Model)
	inputCost := float64(row.InputTokens) / 1_000_000 * p.InputPerMTok
	outputCost := float64(row.OutputTokens) / 1_000_000 * p.OutputPerMTok
	cacheReadCost := float64(row.CacheReadInputTokens) / 1_000_000 * p.CacheReadPerMTok
	cacheWriteCost := float64(row.CacheCreationInputTokens) / 1_000_000 * p.CacheWritePerMTok
	return inputCost + outputCost + cacheReadCost + cacheWriteCost
}
