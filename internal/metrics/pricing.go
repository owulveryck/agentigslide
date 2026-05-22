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
	{"opus", ModelPricing{InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25}},
	{"sonnet", ModelPricing{InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75}},
	{"haiku", ModelPricing{InputPerMTok: 1.0, OutputPerMTok: 5.0, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25}},
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
