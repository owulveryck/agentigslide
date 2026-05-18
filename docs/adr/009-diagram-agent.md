# ADR 009 : Agent Designer de diagrammes

- **Date** : 2026-05-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le systeme actuel genere des presentations en dupliquant des slides templates preformatees et en modifiant leur contenu textuel. Il ne peut pas creer de formes programmatiquement. Cela empeche la generation de diagrammes (flowcharts, architectures, processus) qui sont pourtant un besoin frequent dans les presentations techniques.

Les utilisateurs doivent actuellement creer ces diagrammes manuellement apres generation, ce qui casse le flux automatise.

## Decision

### Agent Designer comme Writer specialise

Un nouvel agent "Designer" s'integre dans le pipeline multi-agent au meme niveau que le Writer. L'orchestrateur dispatche vers le Designer quand `SlideNeed.SlideType == "diagram"`, vers le Writer pour les autres types.

### Auto-layout en Go

Le LLM decrit uniquement la **topologie** du diagramme (noeuds, aretes, groupes) via un tool call structure. Le calcul des positions concretes (coordonnees EMU) est effectue par un moteur de layout en Go utilisant un algorithme en couches (Sugiyama simplifie). Ce choix est motive par l'incapacite des LLMs a produire des coordonnees spatiales precises et coherentes.

### Creation de slides via l'API

Les slides de diagramme sont creees via `CreateSlide` (layout BLANK) au lieu de dupliquer un template existant. Cela simplifie la logique (pas besoin d'un template "vide" dans le catalogue) au prix de la perte du fond/branding OCTO, compensee par l'application des couleurs de la charte aux formes.

### Double review

1. **Pre-rendu** : le Reviewer existant valide la topologie et le contenu du DiagramSpec dans le plan assemble, avec la meme boucle de retry que les Writers.
2. **Post-rendu** : une etape de post-production optionnelle (comme `fixfonts`) exporte l'image de la slide et la soumet a Claude Vision pour detecter les problemes visuels (chevauchements, texte tronque).

## Alternatives evaluees

### Positionnement par le LLM

Le LLM specifie les positions dans une grille ou en coordonnees approximatives. Rejete car les LLMs produisent des layouts incoherents et fragiles, avec des chevauchements frequents.

### Duplication d'un template vierge

Dupliquer une slide template quasi-vide (avec fond/footer OCTO) au lieu de creer via l'API. Rejete car aucune slide appropriee n'existe dans le template actuel, et en creer une cree une dependance au template specifique.

### Utilisation de Mermaid/Graphviz

Parser un format texte (Mermaid) et utiliser un moteur de layout externe (Graphviz). Rejete pour eviter les dependances externes et garder le systeme auto-contenu en Go.

### Review visuel integre au pipeline

Integrer la boucle de review visuel dans le pipeline principal au lieu d'en faire une post-production. Rejete car cela complexifie le pipeline et necessite des appels `ExecutePlan` incrementaux.

## Consequences

### Positives

- **Nouvelles capacites** : le systeme peut generer des diagrammes (flowcharts, architectures) automatiquement
- **Coherence architecturale** : le Designer suit le meme pattern que les autres agents (tool_use, envconfig, metriques)
- **Layout fiable** : le calcul de positions en Go produit des resultats deterministes et lisibles
- **Extensible** : le moteur de layout peut evoluer independamment de l'agent (nouveaux algorithmes, formes)

### Negatives

- **Pas de branding** : les slides diagram n'ont pas le fond/footer OCTO (compensable par les couleurs des formes)
- **Complexite du pipeline** : le Selector et le Reviewer doivent comprendre un nouveau type de slide
- **Limitation des formes** : Google Slides n'a pas de forme DIAMOND native (contournement par rotation)
- **Cout** : la review visuelle post-rendu ajoute un appel Claude Vision par slide diagram

## Fichiers concernes

| Fichier | Modification |
|---------|-------------|
| `internal/agent/types.go` | Types `DiagramSpec`, `DiagramNode`, `DiagramEdge`, `DiagramGroup`; `DiagramSpecs` dans `PipelineState` |
| `internal/model/plan.go` | Types miroir `DiagramSpec`; champ `Diagram` dans `SlideRequest` |
| `internal/diagram/` | Nouveau package : `styles.go`, `layout.go`, `render.go` + tests |
| `internal/agent/designer/` | Nouveau package : `designer.go`, `prompt_designer.txt`, `embed.go`, `visual_review.go` |
| `internal/agent/config.go` | `DesignerModel`, `DiagramVisualReviewModel`, `MaxDiagramVisualRetries` |
| `internal/agent/orchestrator/orchestrator.go` | Dispatch vers Designer, assemblage avec DiagramSpec |
| `internal/agent/outliner/outliner.go` | `"diagram"` dans l'enum `slideType` |
| `internal/agent/selector/selector.go` | Gestion `sourceSlide: -1` pour les diagrammes |
| `internal/agent/reviewer/reviewer.go` | Validation de la topologie des DiagramSpec |
| `internal/pipeline/pipeline.go` | Phase de creation des formes via BatchUpdate |
| `docs/architecture.md` | Documentation du Designer et de la phase diagram |
