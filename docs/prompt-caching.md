# Prompt caching Vertex AI

Le système utilise le cache éphémère de Vertex AI pour partager le contexte système entre les appels parallèles aux Writers. L'implémentation est dans [`internal/agent/cache.go`](../internal/agent/cache.go).

## Principe

Le prompt système (instructions de base + mémoire agent + instructions template) est commun à tous les Writers d'un même pipeline run. Plutôt que de retransmettre ce contexte à chaque appel, Vertex AI le met en cache après le premier appel et les suivants le lisent depuis le cache.

## Implémentation

```go
func BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory string) []vertex.ContentBlock {
    var blocks []vertex.ContentBlock

    blocks = append(blocks, vertex.ContentBlock{
        Type: "text",
        Text: systemPrompt,
    })

    if agentMemory != "" {
        blocks = append(blocks, vertex.ContentBlock{
            Type: "text",
            Text: "MÉMOIRE DE L'AGENT (...) :\n" + agentMemory,
        })
    }

    if templateInstructions != "" {
        blocks = append(blocks, vertex.ContentBlock{
            Type: "text",
            Text: "INSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + templateInstructions,
        })
    }

    // Cache breakpoint sur le dernier bloc
    blocks[len(blocks)-1].CacheControl = &vertex.CacheControl{Type: "ephemeral"}
    return blocks
}
```

Le `CacheControl` avec `Type: "ephemeral"` est placé sur le **dernier bloc système**. Tout le contenu du début jusqu'à ce point est éligible au cache. Les messages utilisateur (spécifiques à chaque slide) viennent après et ne sont pas cachés.

## Impact sur les coûts

Avec 20 slides et un système prompt de ~10 000 tokens :

- **Sans cache** : 20 x 10 000 = 200 000 tokens d'entrée pour le système prompt seul
- **Avec cache** : 10 000 tokens en écriture + 19 x 10 000 en lecture cache (tarif réduit)

Le suivi se fait via le [collecteur de métriques](./metrics.md) qui enregistre `CacheReadInputTokens` et `CacheCreationInputTokens` par appel.

## Voir aussi

- [Métriques](./metrics.md) — suivi des tokens et du cache hit
- [Structured output](./structured-output.md) — format des appels API
