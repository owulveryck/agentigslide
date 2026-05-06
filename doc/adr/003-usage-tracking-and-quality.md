# ADR 003 : Token Usage Tracking et Ameliorations Qualite de Sortie

- **Date** : 2026-05-06
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

L'audit du pipeline multi-agents (ADR 001) a identifie deux axes d'amelioration :

1. **Aucune visibilite sur les couts** : L'API Vertex AI retourne un champ `usage` dans chaque reponse (tokens d'entree, de sortie, cache hits/writes) mais il est entierement ignore. Pour une presentation de 10 slides, le pipeline fait ~13 appels API sans aucune mesure du cout reel.

2. **Qualite de sortie perfectible** :
   - Le Reviewer utilise un appel classique sans reflexion approfondie. L'extended thinking de Claude permettrait un raisonnement plus rigoureux sur les criteres de qualite.
   - Le Writer utilise un schema JSON statique avec un tableau generique de modifications. Le modele peut inventer des noms de champs inexistants, necessitant un post-filtrage (`filterValidFields()`).

## Decisions

### Decision 1 : Parser et loguer le token usage de chaque appel API

Ajouter une struct `Usage` a `FullResponse` dans le client Vertex AI. Chaque agent logue ses metriques de tokens apres chaque appel. Cela donne une visibilite immediate sur les couts sans changer les interfaces existantes.

### Decision 2 : Activer l'extended thinking pour le Reviewer

Le Reviewer utilise le modele le plus puissant (Opus) pour valider la qualite du plan assemble. L'extended thinking lui donne un budget de reflexion interne avant de produire sa revue structuree via `tool_use`.

**Contrainte API** : L'extended thinking impose `temperature = 1.0`. Le Reviewer utilisait `temperature = 0.0` pour le determinisme. Le thinking compense largement : le raisonnement structure contraint la sortie plus efficacement qu'une temperature basse.

**Configuration** : Le budget thinking est configurable via `AGENT_REVIEWER_THINKING_BUDGET` (defaut : 10240 tokens). Mettre a 0 pour desactiver et revenir au comportement precedent.

### Decision 3 : Schema dynamique par Writer

Remplacer le schema JSON statique du Writer (tableau generique `[{variableName, newText}]`) par un schema dynamique ou chaque champ du template est une propriete nommee avec `maxLength` :

Avant :
```json
{"modifications": [{"variableName": "titleShape", "newText": "..."}]}
```

Apres :
```json
{"titleShape": "Mon titre", "bodyShape": "Mon contenu"}
```

Avantages :
- Claude ne peut plus inventer de noms de champs (les proprietes sont fixees dans le schema)
- `maxLength` sert de hint pour chaque champ
- `filterValidFields()` devient inutile
- Le parsing est plus direct (objet plat -> map[string]string)

`enforceMaxChars()` est conserve comme filet de securite car `maxLength` n'est pas strictement enforce par le modele.

## Choix Techniques

### Token Usage : Log local sans changement d'interface

Chaque agent logue son usage via `slog.Info` apres l'appel API. Pas d'agregation dans l'orchestrateur pour cette premiere iteration — zero changement de signature sur les methodes `Run()` / `WriteSlide()`.

### Extended Thinking : Budget configurable, desactivable

`WithThinking(budgetTokens)` force automatiquement `temperature = 1.0` pour respecter la contrainte API. Si le budget est 0, le thinking n'est pas active et la temperature reste configurable.

### Schema Dynamique : Fallback pour slides sans champs

Si un slide n'a aucun champ editable (`len(fields) == 0`), le Writer n'est pas appele (le comportement existant via `filterValidFields` qui vidait les modifications est preserve en amont par le check dans `writeSlides()`).

## Consequences

### Positives

- **Visibilite couts** : Chaque run du pipeline produit des logs de tokens par agent, permettant d'optimiser les modeles et de detecter les anomalies (ex: un Writer qui consomme anormalement).
- **Qualite Reviewer** : L'extended thinking ameliore la detection de problemes subtils (contenu invente, incoherences entre slides) grace a un raisonnement plus approfondi.
- **Robustesse Writer** : Le schema dynamique elimine une classe entiere de bugs (noms de champs inventes) au niveau du schema plutot qu'en post-traitement.
- **Observabilite cache** : Les champs `cache_read_input_tokens` et `cache_creation_input_tokens` permettent de verifier que le prompt caching (ADR 002) fonctionne effectivement.

### Negatives

- **Cout extended thinking** : Le budget thinking (10240 tokens par defaut) augmente le cout du Reviewer. Mitige par la configurabilite (desactivable via env var a 0).
- **Schema dynamique** : Le schema est reconstruit a chaque appel Writer. Impact negligeable (construction JSON en memoire).
- **Temperature Reviewer** : Passer de 0.0 a 1.0 change legerement le comportement du Reviewer. Le thinking compense en structurant le raisonnement.

## Fichiers Concernes

### A modifier

| Fichier | Modification |
|---------|-------------|
| `internal/vertex/types.go` | `Usage`, `ThinkingConfig`, `WithThinking()`, champ `Usage` sur `FullResponse` |
| `internal/vertex/client.go` | Ajouter `thinking` au request body |
| `internal/agent/config.go` | `ReviewerThinkingBudget` |
| `internal/agent/outliner.go` | Log usage |
| `internal/agent/selector.go` | Log usage |
| `internal/agent/writer.go` | Log usage, `buildWriterTool(fields)`, parsing objet plat |
| `internal/agent/reviewer.go` | Log usage, `WithThinking()` conditionnel |
| `internal/agent/orchestrator.go` | Supprimer `filterValidFields()` |
