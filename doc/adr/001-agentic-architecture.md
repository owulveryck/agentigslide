# ADR 001 : Architecture Agentique Multi-Agents pour slidegen

- **Date** : 2026-05-05
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le module `slidegen` genere des presentations Google Slides a partir d'une demande utilisateur en markdown. Le systeme actuel repose sur un **pipeline monolithique** : un seul appel a Claude (via Vertex AI) recoit le catalogue complet des templates (~60 slides, ~15-20KB de texte) ainsi que la demande utilisateur, et doit en un seul passage :

1. Analyser la structure de la demande (sections, sous-sections, nombre d'elements)
2. Selectionner les templates les plus adaptes parmi le catalogue
3. Remplir le contenu textuel de chaque champ en respectant les contraintes de capacite (maxChars)
4. Maintenir la coherence globale de la presentation (pas de duplication, bon enchaineent)

### Limites identifiees

- **Raisonnement en un seul passage** : Pas de boucle de correction. Si Claude choisit un slide a 6 champs pour 3 bullet points, ou depasse une limite de caracteres, l'erreur passe en production.
- **Pas de parallelisme** : Ecrire le contenu du slide 3 et du slide 7 sont des taches independantes, mais tout est fait dans un seul appel sequentiel.
- **Inefficacite tokens** : Chaque sous-tache recoit l'integralite du contexte (catalogue complet) alors que seule une fraction est pertinente.
- **Couplage fort** : Impossible d'utiliser des modeles differents selon la complexite de la tache (un slide de couverture a 2 champs ne necessite pas le meme modele qu'un slide de contenu a 6 champs).

## Decision

Transformer le pipeline monolithique en une **architecture multi-agents** avec 4 agents specialises, coordonnes par un orchestrateur en code Go (pas d'IA).

## Architecture des Agents

### Vue d'ensemble

```
                     +---------------------+
                     |    Orchestrateur     |
                     |  (code Go, pas IA)   |
                     +----------+----------+
                                |
            +-------------------+-------------------+
            |                   |                   |
   +--------v--------+ +-------v--------+ +-------v--------+
   |  1. Outliner    | |  2. Selector   | |  4. Reviewer   |
   |    (Sonnet)     | |    (Sonnet)    | |    (Opus)      |
   +--------+--------+ +-------+--------+ +-------^--------+
            |                  |                   |
            |           +------v------+            |
            |           | 3. Writers  |            |
            |           |  (fan-out)  |            |
            |           | Haiku/Sonn. |            |
            |           +------+------+            |
            |                  |                   |
            |                  +-------------------+
            |
   +--------v--------------------------------+
   |  5. Executor (code Go existant)         |
   |     pipeline.ExecutePlan()              |
   +--------+--------------------------------+
            |
   +--------v--------------------------------+
   |  6. FixFonts (existant, inchange)       |
   +------------------------------------------+
```

### Agent 1 : Outliner (Architecte de Structure)

- **Responsabilite** : Analyser la demande utilisateur et produire un plan de presentation structure (`PresentationOutline`).
- **Entree** : Demande utilisateur (markdown brut). Ne recoit PAS le catalogue de templates.
- **Sortie** : JSON structure avec titre, sections, et pour chaque slide necessaire : intention, elements de contenu extraits, nombre d'items, longueur max, type de slide.
- **Modele** : Claude Sonnet (analyse structurelle, rapide, economique).
- **Justification de l'isolation** : En ne donnant pas le catalogue au modele, on force le raisonnement "de quoi a-t-on besoin ?" avant "qu'a-t-on a disposition ?", evitant le biais de disponibilite des templates.

### Agent 2 : Selector (Selecteur de Templates)

- **Responsabilite** : Pour chaque besoin de slide identifie par l'Outliner, selectionner le meilleur template du catalogue et definir le mapping champs-contenu.
- **Entree** : `PresentationOutline` + catalogue compact des templates (~15-20KB).
- **Sortie** : `SelectionPlan` JSON avec pour chaque slide : le template choisi (`sourceSlide`), l'index du besoin correspondant (`outlineIndex`), et la justification (`rationale`). Le mapping champs/contenu est delegue au Writer.
- **Modele** : Claude Sonnet, temperature 0.1 (matching deterministe).
- **Valeur ajoutee** : Agent specialise dans le matching contraintes/templates. Dispose du contexte `itemCount` et `maxItemLength` de l'Outliner pour des choix informes.

### Agent 3 : Writer(s) (Redacteurs -- Fan-Out Parallele)

- **Responsabilite** : Generer le contenu textuel final pour chaque slide individuellement.
- **Entree** : Par slide -- la `SlideSelection` + les `contentItems` pertinents + metadata des champs (variableNames, roles, maxChars). Pas le catalogue complet.
- **Sortie** : `SlideContent` (les `TextModification` pour ce slide).
- **Modele** : Haiku pour slides simples (cover, intercalaire, <= 2 champs), Sonnet pour slides complexes (> 2 champs).
- **Execution** : Tous les Writers en parallele via goroutines Go avec semaphore configurable.
- **Gains** : Budget tokens minimal par appel (~500B-2KB), parallelisme natif, Haiku pour les cas simples.

### Agent 4 : Reviewer (Critique -- Boucle de Reflexion)

- **Responsabilite** : Valider le plan assemble avant execution. Boucle de self-correction absente du systeme actuel.
- **Entree** : `GenerationPlan` assemble + demande originale + catalogue compact.
- **Sortie** : `ReviewResult` -- approbation ou liste de problemes structures (overflow, duplication, contenu manquant, template inadequat, incoherence).
- **Boucle** : Si des problemes sont detectes, l'Orchestrateur renvoie les issues au Writer concerne **avec le feedback du Reviewer** (description du probleme + suggestion de correction). Le Writer recoit ces issues dans une section "CORRECTIONS DEMANDEES" de son prompt, lui permettant d'ajuster sa reponse en connaissance de cause. Maximum 2 iterations pour borner le cout.
- **Modele** : Claude Opus.

### Orchestrateur (code Go pur)

Pipeline deterministe en Go :

1. **Outliner** : `userRequest` -> `PresentationOutline`
2. **validateOutline()** (Go pur) : verifie coherence structurelle (sections non vides, `itemCount == len(contentItems)`)
3. **Selector** : `PresentationOutline` + catalogue -> `SelectionPlan`
4. **validateSelection()** (Go pur) : verifie `outlineIndex` in range, `sourceSlide` existe dans le catalogue, `variableNames` valides. Les index hors limites sont clampes avec warning.
5. **Writers** (parallele) : pour chaque selection -> `SlideContent` (goroutines avec semaphore)
6. **Assemblage** (Go pur) : combiner les `SlideContent` en `GenerationPlan`
7. **Reviewer** : validation qualite avec boucle de correction (max 2 iterations). Si rejet, les issues sont transmises aux Writers concernes pour correction ciblee.
8. **Enrichissement** (existant) : `plan.EnrichPlan()` -> `PresentationPlan`
9. **Execution** (existant) : `pipeline.ExecutePlan()` -> Google Slides
10. **FixFonts** (existant) : post-processing optionnel

## Choix Technologiques

### Sorties Structurees via tool_use

Chaque agent utilise le mecanisme `tool_use` de Claude (via Vertex AI) pour produire ses sorties JSON. Avec `tool_choice: {"type": "tool"}`, Claude est contraint d'appeler un outil dont le schema JSON correspond a la structure de sortie attendue. Cela remplace l'approche actuelle fragile de parsing JSON dans du texte avec stripping de code fences markdown.

Nouveaux types requis dans `internal/vertex/` :
- `Tool` (name, description, input_schema)
- `ContentBlockFull` (support des blocs tool_use en plus de text)
- `FullResponse` (liste de content blocks + stop_reason)
- Nouvelle methode `RawPredictFull()` retournant la reponse structuree

### Resilience API

Le client Vertex AI integre un mecanisme de retry avec backoff exponentiel sur les erreurs transitoires (HTTP 429, 529, 5xx). Le retry est transparent pour les appelants : jusqu'a 5 tentatives avec delais de 3s, 6s, 12s, 24s, 48s. Tous les agents detectent egalement `stop_reason: "max_tokens"` pour echouer explicitement plutot que de retourner un JSON tronque silencieusement.

### Communication Inter-Agents

Objet etat partage (`PipelineState`) passe par reference via l'orchestrateur. Approche Go-idiomatique pour un DAG pipeline. Mutex pour l'acces concurrent des Writers.

### Variables d'Environnement

Configuration des modeles par agent via le prefix `AGENT` (kelseyhightower/envconfig) :

```bash
AGENT_OUTLINER_MODEL="claude-sonnet-4-6"                # Modele Outliner
AGENT_SELECTOR_MODEL="claude-sonnet-4-6"                # Modele Selector
AGENT_WRITER_MODEL="claude-sonnet-4-6"                  # Modele Writer (slides complexes, >2 champs)
AGENT_WRITER_SIMPLE_MODEL="claude-haiku-4-5@20251001"   # Modele Writer (slides simples, <=2 champs)
AGENT_REVIEWER_MODEL="claude-opus-4-6"                  # Modele Reviewer
AGENT_MAX_PARALLEL=3                                     # Nombre max de Writers en parallele
AGENT_MAX_REVIEW_RETRIES=2                               # Iterations max de la boucle de review
```

## Consequences

### Positives

- **Qualite superieure** : Boucle de reflexion (Reviewer) pour detecter et corriger les erreurs avant execution.
- **Parallelisme** : Les Writers s'executent en parallele, reduisant la latence pour les grandes presentations.
- **Specialisation** : Chaque agent a un prompt optimise pour sa tache specifique.
- **Flexibilite modeles** : Haiku pour les taches simples, Sonnet pour les complexes, configurable par env var.
- **Maintenabilite** : Chaque agent est un module independant, testable unitairement.

### Negatives

- **Surcout tokens** : ~20% de tokens supplementaires dans le happy path (~30KB vs ~25KB), compense par l'utilisation de Haiku et le gain en qualite.
- **Latence additionnelle** : La sequentialite Outliner -> Selector ajoute de la latence (partiellement compensee par le parallelisme des Writers).
- **Complexite** : Plus de code a maintenir (orchestrateur, 4 agents, types intermediaires).

### Backward Compatibility

- Le mode monolithique existant reste le mode par defaut.
- Le mode agentique s'active via `--agent` (flag CLI) ou `SLIDEGEN_AGENT_MODE=true` (env var).
- Les structures de sortie finales (`GenerationPlan`, `PresentationPlan`) restent identiques.
- L'execution (`ExecutePlan`) et le post-processing (`fixfonts`) sont reutilises sans modification.
- Le mode recovery (`--plan plan.json`) et le mode amendment fonctionnent dans les deux modes.

## Evolutions Post-Implementation

### Changements par rapport au design initial

1. **Decouplage Selector/Writer pour le mapping de champs** : Le design initial prevoyait que le Selector produise un mapping `variableName -> contentItem`. En pratique, ce mapping est entierement delegue au Writer qui recoit les `templateFields` et les `contentItems` et decide lui-meme de la repartition. Le Selector ne produit plus qu'un triplet `(outlineIndex, sourceSlide, rationale)`. Ce decouplage reduit la complexite du Selector et donne plus de liberte au Writer.

2. **Ajout de `MaxSelectorRetries`** : Le retry du Selector avec injection des erreurs de validation n'etait pas prevu dans le design initial. Ajoute pour gerer les cas ou Claude choisit un slide inexistant ou inadequat (slide sans champs editables pour du contenu textuel).

3. **Injection de `TemplateInstructions`** : Un fichier PROMPT.md optionnel dans le repertoire du template est injecte dans le system prompt de tous les agents. Permet de specialiser le comportement par template (ex: conventions de nommage, styles).

4. **Post-processing defensif** : `enforceMaxChars()` et `filterValidFields()` ont ete ajoutes dans l'orchestrateur comme filet de securite apres chaque Writer, en plus des contraintes dans les prompts. Approche "trust but verify".

### Retour d'experience (audit mai 2026)

**Ce qui fonctionne bien :**
- L'isolation Outliner/catalogue force un raisonnement structurel de qualite avant le matching
- Le parallelisme des Writers reduit significativement la latence sur les grandes presentations
- La boucle Reviewer -> Writer avec feedback cible corrige efficacement les problemes d'overflow et de duplication
- La selection adaptative de modele (Haiku/Sonnet) selon la complexite du slide est un bon compromis cout/qualite
- La degradation gracieuse (continuer si le Reviewer echoue) est pragmatique pour un outil de productivite

**Axes d'amelioration identifies :**
- Couverture de tests insuffisante sur les agents (seuls validate.go et le parsing sont testes)
- Pas de validation que le nombre de selections couvre tous les besoins de l'outline
- Observabilite limitee (pas de correlation ID, pas de metriques tokens)
- Prompt caching Vertex AI non exploite (system prompts et catalogue identiques entre appels)

## Fichiers Concernes

### A creer

| Fichier | Role |
|---------|------|
| `internal/agent/types.go` | Structures intermediaires (PresentationOutline, SelectionPlan, SlideContent, ReviewResult) |
| `internal/agent/config.go` | AgentConfig avec envconfig prefix "AGENT" |
| `internal/agent/orchestrator.go` | Orchestrateur Go, coordination, boucle de retry |
| `internal/agent/outliner.go` | Agent Outliner |
| `internal/agent/selector.go` | Agent Selector |
| `internal/agent/writer.go` | Agent Writer (fan-out parallele) |
| `internal/agent/reviewer.go` | Agent Reviewer |
| `internal/agent/validate.go` | Validations programmatiques inter-etapes (outline, selection) |

### A modifier

| Fichier | Modification |
|---------|-------------|
| `internal/vertex/types.go` | Ajouter Tool, ContentBlockFull, FullResponse, options tool_use |
| `internal/vertex/client.go` | Ajouter RawPredictFull() avec support tool_use |
| `slidegen/main.go` | Ajouter flag `--agent` / env var pour router vers l'orchestrateur |
