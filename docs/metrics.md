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

## Durees par phase (ADR 019)

Le collector accumule les durees wall-clock par phase via `Collector.AddPhaseDuration(phase, d)` :
`outline`, `selection`, `writers`, `pre-review`, `review` (orchestrateur) et `execution`,
`formatter-1`, `visual-review`, `formatter-2` (CLI). Le `Summary` les expose dans
`PhaseDurations`, et chaque `RunRecord` de `~/.slidegen/metrics_history.jsonl` les persiste
en secondes (`phaseDurations`). Objectif : Σ phases / duree totale ≥ 95 %, et detection des
regressions de duree des phases non-LLM run-over-run.

Pour l'analyse comparative d'un run trace, voir `cmd/traceeval` (ADR 025).

## Ledger LLM exhaustif et gates de regression (ADR 032)

En fin de run, le contenu complet du `Collector` (un enregistrement par appel LLM :
agent, modele reel, tokens in/out, cache read/write, duree) est dumpe dans la trace
debug sous `agentCalls` via `Collector.Calls()`. C'est la source autoritaire du cout :
elle couvre les appels absents des sections par phase (visual review, memory synthesis,
designer).

`cmd/traceeval` calcule alors le **cout reel** par appel (4 composantes de prix dont
cache read x0,1 et cache write x1,25) et le **cache hit ratio**. Le mode
`traceeval -gate baseline.json new.json` echoue (exit != 0) sur violation d'invariants
(depassements non corriges, rejeux deterministes en desaccord, erreurs pipeline,
selection sanitizee) ou regression relative (cout > baseline +15 %, findings visuels
non resolus en hausse, iterations de review > baseline+1). Les traces de reference
vivent dans `traces/golden/`. Combine avec `buildindex -check` (ADR 027), la CI verifie
le pipeline a cout API nul.
