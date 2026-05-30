# Métriques et observabilité

Le pipeline collecte des métriques détaillées sur chaque appel d'agent pour le suivi des coûts, la performance et le diagnostic. L'implémentation est dans [`internal/metrics/metrics.go`](../internal/metrics/metrics.go).

## Données collectées par appel

```go
type AgentCall struct {
    Agent                    string  // "outliner", "selector", "writer", "reviewer", "designer"
    Model                    string  // e.g. "claude-sonnet-4-6"
    InputTokens              int
    OutputTokens             int
    CacheReadInputTokens     int     // Tokens lus depuis le cache
    CacheCreationInputTokens int     // Tokens écrits pour créer le cache
}
```

## Agrégation

Le `Collector` agrège les appels en `AgentRow` par couple (agent, modèle) :

```go
type AgentRow struct {
    Agent                    string
    Model                    string
    Calls                    int       // Nombre d'appels API
    InputTokens              int       // Somme
    OutputTokens             int       // Somme
    CacheReadInputTokens     int       // Somme
    CacheCreationInputTokens int       // Somme
    Cost                     float64   // Estimation via tarifs par modèle
}
```

## Métriques pipeline

En plus des métriques par agent, le `Collector` suit :

- **`SelectorRetries`** : nombre de retries du Selector (échecs de validation)
- **`ReviewerRetries`** : nombre d'itérations de la [boucle de feedback](./review-feedback-loop.md)
- **`SlidesGenerated`** : nombre final de slides produites
- **`PipelineDuration`** : durée totale du pipeline

## Thread-safety

Le `Collector` utilise un `sync.Mutex` car les Writers parallèles appellent `Record()` concurremment.

## Voir aussi

- [Prompt caching](./prompt-caching.md) — ce que mesurent `CacheReadInputTokens` et `CacheCreationInputTokens`
- [PipelineState](./pipeline-state.md) — état du pipeline dont les métriques sont un reflet
