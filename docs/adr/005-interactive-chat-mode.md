# ADR 005 : Mode Chat Interactif pour le Raffinement de l'Outline

- **Date** : 2026-05-07
- **Statut** : Accepte (supersede partiellement par [ADR 006](006-default-agent-chat-mode.md) : le mode chat est desormais le comportement par defaut, le flag `--chat` a ete supprime)
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline multi-agent (ADR 001) execute ses 5 etapes de maniere entierement automatique : l'Outliner produit une structure de presentation, le Selector choisit les templates, les Writers redigent le contenu, et le Reviewer valide. L'utilisateur soumet sa demande (`request.md`) et ne revoit la presentation qu'a la fin, une fois creee dans Google Slides.

### Limites identifiees

- **Pas de controle utilisateur sur la structure** : L'Outliner decide seul du decoupage en sections et du nombre de slides. Si la structure ne convient pas (trop de slides, section manquante, mauvais decoupage), l'utilisateur doit relancer toute la generation.
- **Cout des iterations** : Une execution complete du pipeline (5 agents) coute ~30-50K tokens. Corriger une structure inadaptee en relancant le pipeline complet est couteux en tokens et en temps.
- **Gap entre intention et interpretation** : La demande markdown est souvent ambigue sur le niveau de detail souhaite par section. L'Outliner fait un choix que l'utilisateur ne peut pas influencer avant l'execution.
- **Pas d'entree interactive** : L'utilisateur doit preparer un fichier markdown complet avant de lancer la generation. Pas de moyen de decrire sa presentation de maniere conversationnelle.

## Decision

Ajouter un mode interactif (`--chat`) qui permet a l'utilisateur de raffiner l'outline produit par l'Outliner via une boucle conversationnelle multi-tour, **avant** que le reste du pipeline ne s'execute. L'outline approuve est ensuite injecte directement dans l'orchestrateur, qui saute l'etape Outliner automatique.

## Architecture

### Flux du mode chat

```
readChatInput() / --file
       |
       v
OutlinerAgent.RunInteractive(userRequest, feedbackFn)
       |
       +---> [Claude Sonnet] produce_outline (round 1)
       |         |
       |    feedbackFn() : affiche l'outline, lit le feedback
       |         |
       |    feedback vide ? -----> outline approuve
       |         |                      |
       |    feedback texte             v
       |         |            orchestrator.Outline = outline
       |         v            orchestrator.Generate()
       +---> [Claude Sonnet] produce_outline (round N)    (saute l'etape 1/5)
              avec historique complet de la conversation
```

### Integration dans le pipeline existant

Le mode chat se branche **avant** l'orchestrateur sans modifier le pipeline lui-meme :

1. `main.go` cree un `OutlinerAgent` independant et appelle `RunInteractive()`
2. L'outline approuve est assigne a `orchestrator.Outline`
3. `orchestrator.Generate()` detecte `o.Outline != nil` et saute l'etape 1/5 (Outliner automatique)
4. Les etapes 2-5 (Selector, Writers, Assembler, Reviewer) s'executent normalement avec l'outline pre-approuve

## Choix Techniques

### Conversation multi-tour via l'API Vertex AI

L'Outliner dispose deja d'un mecanisme `tool_use` force (`produce_outline`). Le mode interactif reutilise ce mecanisme en construisant un historique de messages multi-tour :

- **Tour 1** : `[user: "Analyse cette demande..."]` -> Claude produit l'outline via `produce_outline`
- **Tour N** : L'historique inclut la reponse precedente (blocs `tool_use` echoes en message `assistant`) + un acquittement `tool_result` + le feedback utilisateur en message `user`

Les helpers `ToolUseContentBlock()` et `ToolResultContentBlock()` dans `internal/vertex/types.go` construisent les blocs de reponse pour respecter le protocole `tool_use` de Claude (chaque `tool_use` doit etre suivi d'un `tool_result`).

### Protocole d'approbation

La callback `feedbackFn` retourne :
- `""` (chaine vide) si l'utilisateur approuve : mots-cles reconnus (`"ok"`, `"done"`, `"yes"`, `"y"`, `"go"`, `"lance"`, `"lgtm"`) ou ligne vide (Enter)
- Le texte du feedback sinon, pour raffinement

Ce design decouple l'interface utilisateur (terminal, potentiellement web) de la logique de conversation.

### Entree utilisateur interactive avec references `@fichier`

La fonction `readChatInput()` permet une saisie multi-ligne depuis stdin :
- Chaque ligne est traitee individuellement
- Une ligne vide termine la saisie
- Les references `@chemin` sont expansees en contenu du fichier reference (via regex `@(\S+)`)
- Le feedback de chaque tour beneficie aussi de l'expansion `@fichier`

Cela permet de combiner du texte libre et du contenu de fichiers sans preparer un markdown complet.

### Affichage de l'outline

`FormatOutline()` dans `outline_display.go` produit un rendu texte lisible en terminal :
- Titre de la presentation
- Sections numerotees avec leur intention
- Pour chaque slide : type, intent, nombre d'items
- Preview du contenu (tronque a 80 caracteres)
- Total des slides et sections

### Mutualite exclusive avec `--web`

Le mode chat et le mode web servent le meme objectif (interaction utilisateur avant generation) mais par des canaux differents (terminal vs. navigateur). Ils sont mutuellement exclusifs pour eviter toute ambiguite sur la source d'entree.

### Implication du flag `--agent`

Le flag `--chat` implique automatiquement `--agent` puisque le mode chat ne fonctionne qu'avec le pipeline multi-agent (l'outline pre-construit n'est utilisable que par l'orchestrateur agentique).

## Consequences

### Positives

- **Controle utilisateur** : L'utilisateur peut ajuster la structure (ajouter/retirer des sections, modifier le decoupage) avant que le pipeline ne s'execute, evitant des regenerations completes couteuses.
- **Economie de tokens** : Un raffinement d'outline coute ~2-5K tokens par tour (Sonnet), contre ~30-50K tokens pour une relance complete du pipeline.
- **Saisie flexible** : L'expansion `@fichier` permet de composer la demande a partir de fragments, sans preparer un fichier markdown complet.
- **Zero impact sur le pipeline** : Le mode chat ne modifie ni l'orchestrateur ni les agents existants. Il s'insere comme une etape optionnelle en amont.
- **Decouplage UI/logique** : La callback `feedbackFn` permet de brancher differentes interfaces (terminal, web) sur la meme logique de conversation.

### Negatives

- **Latence additionnelle** : Chaque tour de raffinement ajoute un aller-retour API (~3-8s par tour). Compense par l'economie de ne pas relancer le pipeline complet.
- **Historique de conversation croissant** : Les messages s'accumulent a chaque tour. En pratique, 2-3 tours suffisent pour converger, et le prompt caching (ADR 002) amortit le cout du prefixe repete.
- **Pas de persistance de session** : Si l'utilisateur quitte pendant le raffinement, la conversation est perdue. Acceptable pour un outil CLI interactif.

## Fichiers Concernes

### Crees

| Fichier | Role |
|---------|------|
| `internal/agent/outline_display.go` | `FormatOutline()` : rendu texte de l'outline pour le terminal |

### Modifies

| Fichier | Modification |
|---------|-------------|
| `slidegen/main.go` | Ajout du flag `--chat`, fonctions `readChatInput()`, `expandFileRefs()`, `readOutlineFeedback()`, branchement avant l'orchestrateur |
| `internal/agent/outliner.go` | Ajout de `RunInteractive()` : boucle conversationnelle multi-tour avec `feedbackFn` callback |
| `internal/agent/orchestrator.go` | Ajout du champ `Outline *PresentationOutline` : permet de sauter l'etape Outliner si l'outline est pre-construit |
| `internal/vertex/types.go` | Ajout des helpers `ToolUseContentBlock()` et `ToolResultContentBlock()` pour construire les messages multi-tour |

### Inchanges

| Fichier | Raison |
|---------|--------|
| `internal/agent/selector.go` | Le Selector recoit le meme `PresentationOutline`, quelle que soit sa source |
| `internal/agent/writer.go` | Aucun changement, travaille sur les selections |
| `internal/agent/reviewer.go` | Aucun changement, valide le plan assemble |
| `internal/agent/cache.go` | Prompt caching preservee (ADR 002) |
