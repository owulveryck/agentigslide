# ADR 019 : Observabilite par phase et budgets temps du pipeline complet

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

L'analyse de la trace `edito-trace.json` (generation de 29 slides depuis `edito.md`) a revele un ecart majeur entre le temps total et le temps attribuable :

- Wall-clock total : **6 757 s (1 h 53)**.
- Travail agent trace (outline, selection, writers, review) : **~667 s (10 %)**.
- **90 % du temps est invisible** : les phases execution (Google Slides API), formatter (×2) et visual review (×2) ne portaient aucun champ de duree dans la trace.

De plus, ces phases aval n'etaient pas bornees :

- `AGENT_PIPELINE_TIMEOUT` (10 min) ne couvre que `Orchestrator.Generate()` — le run reel a dure 1 h 53 sans violer ce timeout.
- `executePresentation`, `runFormatter` et `runVisualReview` tournaient sur `context.Background()` sans limite.
- Le telechargement des thumbnails (`fetchThumbnailData`) utilisait un `http.Get` nu : sans timeout client, un fetch bloque pouvait suspendre indefiniment une passe de visual review.

Principe directeur : **on n'optimise pas ce qu'on n'attribue pas**. Toute optimisation de vitesse (phases 1 a 4 du plan d'amelioration) doit etre mesurable contre une attribution complete du wall-clock.

## Decision

### 1. Attribution de chaque phase dans la trace

- Nouveau type `PhaseTrace{Name, StartedAt, DurationMs}` et champ `Phases []PhaseTrace` dans `TraceFile`.
- Nouvelle methode `Tracer.RecordPhase(name string, start time.Time)` (nil-safe, comme le reste du tracer).
- Phases enregistrees : `outline`, `selection`, `writers`, `pre-review`, `review` (orchestrateur) ; `execution`, `formatter-1`, `visual-review`, `formatter-2`, `memory-synthesis` (CLI).
- Champs de duree ajoutes aux traces existantes : `ExecutionTrace.DurationMs`, `FormatterTrace.DurationMs`, `VisualReviewTrace.DurationMs`, et par slide `VisualFindingTrace.ThumbnailFetchMs` / `ReviewMs`.

### 2. Budgets temps par phase

Nouvelles variables de configuration (prefixe `AGENT`) :

| Variable | Defaut | Couvre |
|---|---|---|
| `AGENT_EXECUTION_TIMEOUT` | `15m` | Execution Google Slides API (copy, duplicate, batchUpdate, diagrammes) |
| `AGENT_VISUAL_REVIEW_TIMEOUT` | `15m` | Une passe complete de visual review, corrections incluses |
| `AGENT_FORMATTER_TIMEOUT` | `5m` | Une passe de formatter |

Le helper `agent.PhaseContext(parent, timeout)` retourne un contexte borne (ou le parent inchange si timeout = 0). Les trois fonctions du CLI utilisent desormais un contexte borne au lieu de `context.Background()`.

### 3. Durcissement du fetch de thumbnails

`fetchThumbnailData` utilise un `http.Client{Timeout: 30s}` partage, une requete liee au contexte, et 3 tentatives avec backoff sur les statuts 429/5xx et les erreurs reseau. Un thumbnail inaccessible degrade en `Approved: true` (comportement existant) au lieu de bloquer la passe.

### 4. Durees par phase dans l'historique de metriques

- `Collector.AddPhaseDuration(phase, d)` accumule les durees par phase (les passes repetees s'additionnent).
- `Summary.PhaseDurations` et `RunRecord.PhaseDurations` (secondes) dans `~/.slidegen/metrics_history.jsonl` rendent les regressions de phases non-LLM visibles run-over-run.

## Choix Technologiques

- Reutilisation du pattern nil-receiver du tracer (ADR 017) : zero overhead quand `--trace` est absent.
- Pas de dependance nouvelle : `time`, `context`, `http.Client` standard ; le retry de thumbnail reste local (3 tentatives simples) plutot que `internal/retry` qui est specialise sur `googleapi.Error`.
- Les noms de phases sont des chaines stables — elles constituent le contrat de l'attribution (utilise par le futur harness `traceeval`, ADR 025).

## Conséquences

### Positives

- **Le critere « Σ durees de phases / wall-clock ≥ 95 % » devient verifiable** sur le prochain run du benchmark `edito.md`.
- Un fetch de thumbnail bloque ne peut plus suspendre le pipeline : la passe degrade ou expire.
- L'incoherence « timeout configure 10 min / run reel 1 h 53 » est resolue : chaque phase a son budget.
- L'historique de metriques permet d'identifier la phase responsable d'une regression de duree.

### Negatives / risques

- Un timeout de phase trop court peut interrompre un run legitime sur un grand deck — les defauts (15 m / 15 m / 5 m) sont volontairement larges et ajustables par variable d'environnement.
- L'attribution revele mais ne corrige pas : si la phase dominante est l'API Google (quota/backoff), le levier sera le batching dans `pipeline.go` (risque R1 du plan d'amelioration).

## Fichiers Concernés

- `internal/trace/types.go` — `PhaseTrace`, `Phases`, champs de duree, timeouts dans `ConfigTrace`
- `internal/trace/trace.go` — `RecordPhase`
- `internal/agent/config.go` — `ExecutionTimeout`, `VisualReviewTimeout`, `FormatterTimeout`, `PhaseContext`
- `internal/agent/orchestrator/orchestrator.go` — enregistrement des phases outline/selection/writers/pre-review/review
- `cmd/slidegen/main.go` — phases execution/formatter-N/visual-review/memory-synthesis, contextes bornes
- `cmd/slidegen/agent.go` — timeouts dans `ConfigTrace`
- `internal/pipeline/pipeline.go` — `ExecutionTrace.DurationMs`
- `internal/pipeline/edit_visual_review.go` — client HTTP borne, retry, chronometrage par slide
- `internal/metrics/metrics.go`, `internal/metrics/history.go` — `PhaseDurations`
