# ADR 002 : Prompt Caching Explicite via Vertex AI

- **Date** : 2026-05-06
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline multi-agents (ADR 001) effectue de multiples appels a Claude via Vertex AI au sein d'une meme generation de presentation. Plusieurs blocs de contenu sont identiques entre ces appels :

| Contenu partage | Taille estimee | Utilise par |
|-----------------|----------------|-------------|
| System prompt Writer | ~700 chars | N Writers en parallele |
| System prompt Selector | ~850 chars | 1-3 appels (retries) |
| System prompt Reviewer | ~600 chars | 1-3 appels (retries) |
| TemplateInstructions (PROMPT.md) | Variable | Tous les agents |

Pour une presentation de 10 slides, le pipeline effectue au minimum 13 appels API (1 Outliner + 1 Selector + 10 Writers + 1 Reviewer). Les Writers en particulier partagent exactement le meme system prompt et les memes template instructions.

### Cout sans caching

Chaque appel re-envoie l'integralite du system prompt comme nouveaux tokens d'entree. Pour N slides, le system prompt du Writer est facture N fois au prix plein.

### Opportunite

Anthropic propose un mecanisme de **prompt caching explicite** via le champ `cache_control: {"type": "ephemeral"}` sur les content blocks. Le contenu cache est facture a 0.1x le prix d'entree standard lors des lectures suivantes (cache hit). Le premier appel paie 1.25x (cache write).

## Decision

Implementer le prompt caching explicite pour tous les agents du pipeline en utilisant le format array pour les system prompts avec des breakpoints `cache_control`.

## Choix Techniques

### Format Array pour le System Prompt

L'API Anthropic supporte deux formats pour le system prompt :
- **String** (format actuel) : `"system": "Tu es un expert..."`
- **Array de content blocks** (nouveau) : `"system": [{"type": "text", "text": "Tu es un expert...", "cache_control": {"type": "ephemeral"}}]`

Le format array est necessaire pour attacher `cache_control` aux blocs individuels. Le format string reste supporte pour les appels sans caching (backward compatibility).

### Placement du Breakpoint

Le `cache_control` est place sur le **dernier bloc** du system prompt. Anthropic cache tout le contenu depuis le debut jusqu'au breakpoint inclus. Deux cas :

1. **Avec templateInstructions** : system prompt = bloc 1 (prompt agent) + bloc 2 (instructions template avec `cache_control`)
2. **Sans templateInstructions** : system prompt = bloc 1 (prompt agent avec `cache_control`)

### Pas de Header Supplementaire

Le prompt caching explicite est GA (General Availability) sur Vertex AI. L'en-tete `anthropic-beta` n'est pas necessaire. Le `anthropic_version: "vertex-2023-10-16"` existant suffit.

### Seuils Minimaux

Le caching ne s'active que si le contenu au-dessus du breakpoint atteint un minimum de tokens :
- Claude Sonnet : 1 024 tokens (~4KB de texte)
- Claude Opus : 4 096 tokens (~16KB de texte)
- Claude Haiku : 4 096 tokens (~16KB de texte)

Les system prompts seuls (~700 chars) sont sous le seuil. Avec templateInstructions (~1-5KB), le seuil Sonnet est generalement atteint. Le caching echoue silencieusement si le seuil n'est pas atteint — pas d'impact negatif.

## Consequences

### Positives

- **Reduction du cout tokens** : Les Writers en parallele beneficient de cache hits sur le system prompt + instructions. Pour 10 slides : 1 write a 1.25x + 9 reads a 0.1x = ~2.15x au lieu de 10x.
- **Reduction de latence potentielle** : Les cache hits reduisent le temps de pre-remplissage (prefill) sur l'infrastructure Anthropic.
- **Transparent** : Si le caching echoue (contenu sous le seuil), le comportement est identique a avant — pas de regression.

### Negatives

- **Complexite du format system** : Le code doit gerer deux formats (string et array). Mitige par une fonction helper `buildSystemBlocks()`.
- **Surcout sur le premier appel** : Le cache write coute 1.25x au lieu de 1x. Rentabilise des le deuxieme appel avec le meme prefix.

### Backward Compatibility

- L'option `WithSystem(string)` existante continue de fonctionner (envoi comme string, pas de caching).
- Les CLIs hors pipeline agent (`analyzeSlides`, `fixfonts`) ne sont pas modifies.

## Fichiers Concernes

### A creer

| Fichier | Role |
|---------|------|
| `internal/agent/cache.go` | Helper `buildSystemBlocks()` pour construire les blocs system avec cache_control |

### A modifier

| Fichier | Modification |
|---------|-------------|
| `internal/vertex/types.go` | Ajouter `CacheControl`, champ sur `ContentBlock`, `SystemBlocks` dans options, `WithSystemBlocks()` |
| `internal/vertex/client.go` | `doRequest()` : envoyer system comme array si `SystemBlocks` defini |
| `internal/agent/outliner.go` | Utiliser `WithSystemBlocks(buildSystemBlocks(...))` |
| `internal/agent/selector.go` | Idem |
| `internal/agent/writer.go` | Idem |
| `internal/agent/reviewer.go` | Idem |
