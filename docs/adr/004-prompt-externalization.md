# ADR 004 : Externalisation des Prompts via go:embed et Go Templates

- **Date** : 2026-05-07
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Les prompts systeme (instructions envoyees a Claude via Vertex AI) sont definis comme constantes Go (`const`) ou chaines inline directement dans le code source. Cela pose plusieurs problemes :

- **Lisibilite** : Les prompts longs en francais (jusqu'a 80 lignes pour le Writer) sont melanges avec la logique metier Go, rendant les deux plus difficiles a lire et maintenir.
- **Editabilite** : Modifier un prompt necessite de toucher au code Go, avec les contraintes d'echappement des raw strings et le risque d'introduire des erreurs de syntaxe Go.
- **Contributeurs non-developpeurs** : Un expert metier souhaitant ameliorer un prompt doit naviguer dans du code Go.
- **Fragile injection dynamique** : Les prompts pipeline utilisent `fmt.Sprintf` avec des `%s` positionnels — l'ordre des arguments n'est pas auto-documentant et une erreur est silencieuse.

Le projet utilise deja `//go:embed` en 3 endroits (`analyzeSlides/prompt_analyze.txt`, `internal/monitor/dashboard.html`, `internal/templateindex/stopwords_fr.txt`), etablissant un precedent.

### Inventaire

| Prompt | Fichier source | Type |
|--------|---------------|------|
| Outliner system prompt | `internal/agent/outliner.go` | Statique |
| Selector system prompt | `internal/agent/selector.go` | Statique |
| Writer system prompt | `internal/agent/writer.go` | Statique |
| Reviewer system prompt | `internal/agent/reviewer.go` | Statique |
| Default generation prompt | `internal/pipeline/pipeline.go` | Dynamique (2x `%s`) |
| Amend prompt | `internal/pipeline/pipeline.go` | Dynamique (3x `%s`) |
| Fixfonts analysis prompt | `internal/fixfonts/fixfonts.go` | Dynamique (1x `%s`) |
| MCP tool description | `mcp-server/main.go` | Statique |

## Decision

Externaliser tous les prompts dans des fichiers texte co-localises avec leur code consommateur, embarques via `//go:embed`. Deux categories :

1. **Prompts statiques** (agents, MCP) : fichiers `.txt` embarques comme `string`, aucun traitement supplementaire.
2. **Prompts dynamiques** (pipeline, fixfonts) : fichiers `.txt.tmpl` utilisant `text/template` avec des placeholders nommes et des blocs conditionnels.

## Choix Techniques

### Fichiers `.txt` pour les prompts statiques

Les 4 prompts agents n'ont aucune partie dynamique. L'inclusion conditionnelle de `PROMPT.md` (instructions specifiques au template) est geree par `buildSystemBlocks()` au niveau des content blocks API, ce qui est necessaire pour le prompt caching (cf ADR 002). Ce mecanisme reste inchange.

Convention : `prompt_<role>.txt`, regroupes dans un fichier `embed.go` par package (pattern identique a `internal/monitor/embed.go`).

### Fichiers `.txt.tmpl` pour les prompts dynamiques

Les prompts pipeline et fixfonts utilisaient `fmt.Sprintf` avec des `%s` positionnels. La migration vers `text/template` apporte :

- **Placeholders nommes** : `{{.TemplateIndex}}` au lieu de `%s` — auto-documentant, pas de dependance a l'ordre des arguments.
- **Blocs conditionnels** : `{{if .ExtraInstructions}}...{{end}}` pour l'inclusion de PROMPT.md, remplacant la concatenation manuelle dans `BuildPrompt()`.

Les templates sont parses a l'init via `template.Must()` pour un fail-fast au demarrage.

### Validation des marqueurs obligatoires

Pour prevenir les erreurs silencieuses (un prompt custom ou modifie qui oublie d'inclure le catalogue de templates), une validation pre-rendu verifie que tous les marqueurs obligatoires (`{{.TemplateIndex}}`, `{{.UserRequest}}`, etc.) sont presents dans le contenu brut du template.

- A l'init pour les templates embarques : `panic` si un marqueur manque (ne devrait jamais arriver en build release).
- Au runtime pour les templates custom (flag `--prompt`) : erreur retournee avec message explicite.

### Preservation du prompt caching (ADR 002)

`buildSystemBlocks()` dans `cache.go` n'est pas modifiee. Les prompts agents sont injectes comme avant — seule la source change (embed au lieu de const). La strategie deux blocs avec `cache_control` sur le dernier est preservee integralement.

## Consequences

### Positives

- **Lisibilite** : Les prompts sont des fichiers texte purs, editables avec n'importe quel editeur.
- **Maintenabilite** : Les modifications de prompt n'impliquent plus de code Go.
- **Coherence** : Tous les prompts suivent le meme pattern que `analyzeSlides/prompt_analyze.txt`.
- **Placeholders nommes** : `{{.TemplateIndex}}` est auto-documentant, contrairement au 2e `%s`.
- **Validation proactive** : Les marqueurs obligatoires manquants sont detectes au demarrage ou a l'utilisation du flag `--prompt`, pas silencieusement en production.

### Negatives

- **Dependance `text/template`** : stdlib, pas de risque de supply chain, mais syntaxe a connaitre pour les contributeurs qui modifient les prompts dynamiques.
- **Migration du flag `--prompt`** : Les utilisateurs avec des prompts custom doivent migrer de `%s` vers `{{.FieldName}}`. Breaking change documente.
- **Fichiers supplementaires** : 8 fichiers de prompts + 2 `embed.go` ajoutent du volume au repository (mais pas de code executable supplementaire).

## Fichiers Concernes

### Crees

| Fichier | Role |
|---------|------|
| `internal/agent/prompt_outliner.txt` | System prompt Outliner |
| `internal/agent/prompt_selector.txt` | System prompt Selector |
| `internal/agent/prompt_writer.txt` | System prompt Writer |
| `internal/agent/prompt_reviewer.txt` | System prompt Reviewer |
| `internal/agent/embed.go` | Declarations `//go:embed` pour les 4 prompts agents |
| `internal/pipeline/prompt_amend.txt.tmpl` | Template Go pour le prompt d'amendment |
| `internal/pipeline/embed.go` | Declarations `//go:embed`, parsing et validation des templates pipeline |
| `internal/fixfonts/prompt_fixfonts.txt.tmpl` | Template Go pour le prompt d'analyse de formatage |
| `mcp-server/tool_description.txt` | Description de l'outil MCP |

### Modifies

| Fichier | Modification |
|---------|-------------|
| `internal/agent/outliner.go` | Suppression `const outlinerSystemPrompt` |
| `internal/agent/selector.go` | Suppression `const selectorSystemPrompt` |
| `internal/agent/writer.go` | Suppression `const writerSystemPrompt` |
| `internal/agent/reviewer.go` | Suppression `const reviewerSystemPrompt` |
| `internal/pipeline/pipeline.go` | Suppression consts, nouvelle struct `AmendPromptData`, refactoring de `BuildAmendPrompt` |
| `internal/fixfonts/fixfonts.go` | Remplacement du prompt inline par template embed |
| `mcp-server/main.go` | Embed `toolDescription` |
| `slidegen/main.go` | Mise a jour appels `BuildAmendPrompt` avec structs, support `BuildPromptCustom` pour `--prompt` |

### Inchanges

| Fichier | Raison |
|---------|--------|
| `internal/agent/cache.go` | Preservee integralement (prompt caching ADR 002) |
| `analyzeSlides/prompt_analyze.txt` | Deja externalise, pattern de reference |
