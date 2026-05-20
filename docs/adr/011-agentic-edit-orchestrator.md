# ADR 011 : Orchestrateur agentique pour le mode edition

- **Date** : 2026-05-20
- **Statut** : Propose
- **Decideurs** : Olivier Wulveryck

## Contexte

Le mode edition (`--presentation`) introduit par l'ADR 010 utilise un agent monolithique `EditPlanner` qui recoit la structure de la presentation, la demande de modification et le catalogue de templates, puis produit en un seul appel API un `EditPlan` complet incluant toutes les decisions structurelles ET tout le contenu textuel genere.

Ce design monolithique presente trois limites :

1. **Pas de differenciation de modele** : le meme modele (Sonnet) est utilise pour tout, qu'il s'agisse d'un simple changement de titre (ou Haiku suffirait) ou d'une restructuration complexe.
2. **Pas de parallelisme** : la generation de contenu textuel pour toutes les operations se fait sequentiellement dans un seul appel API, meme quand les operations sont independantes.
3. **Pas de revue qualite** : contrairement au pipeline de creation (ADR 001) qui dispose d'un Reviewer avec boucle de correction, le mode edition n'a aucun controle qualite automatique.

Le pipeline de creation demontre deja l'architecture cible avec une decomposition en agents specialises (Outliner, Selector, Writers paralleles, Reviewer) et une selection de modele par complexite.

## Decision

### Decomposition en modules (Parnas)

Le pipeline d'edition est decompose en quatre modules dont chacun cache ses decisions internes :

1. **EditPlanner** (decisions structurelles) — analyse la presentation et la demande, produit un `EditSkeleton` contenant les operations avec des *intentions* au lieu du contenu textuel final. Ne genere aucun texte.

2. **EditWriter** (nouvel agent, generation de contenu pour `modify_content`) — recoit le texte actuel d'un element et l'intention du planner, produit le nouveau texte. Les modifications d'une meme slide sont regroupees dans un seul appel pour maintenir la coherence.

3. **Writer existant** (reutilise pour `replace_slide` et `insert_slide`) — deja present dans le pipeline de creation, genere le contenu pour les champs d'un template selectionne. Reutilise tel quel.

4. **EditReviewer** (validation qualite, optionnel) — verifie la fidelite aux intentions, la coherence avec le contenu non modifie, et l'absence de sur-modification. Boucle de correction comme le Reviewer du pipeline de creation.

### Selection de modele par complexite

Chaque agent utilise un modele adapte a la complexite de sa tache :

| Agent | Critere | Modele par defaut |
|-------|---------|-------------------|
| EditPlanner | Toujours | Sonnet (raisonnement structurel) |
| EditWriter | <= 2 modifications | Haiku (cout/latence optimaux) |
| EditWriter | > 2 modifications | Sonnet (raisonnement complexe) |
| Writer (replace/insert) | <= 2 champs template | Haiku |
| Writer (replace/insert) | > 2 champs template | Sonnet |
| EditReviewer | Quand active | Opus (validation qualite) |

### Type intermediaire EditSkeleton

Un nouveau type `EditSkeleton` sert de representation intermediaire entre le planner et les writers. Il contient des `ModificationIntent` (ObjectID + intention textuelle) au lieu de `TextModification` (ObjectID + texte final). Le type `EditPlan` existant reste inchange — c'est le contrat avec `pipeline.ExecuteEditPlan()`.

### Mode interactif sur le skeleton

Le feedback utilisateur porte sur le skeleton (decisions structurelles et intentions), pas sur le texte genere. Cela permet des iterations plus rapides et moins couteuses : seul le planner est re-invoque a chaque feedback, sans generation de texte inutile.

### EditOrchestrator

Un nouvel orchestrateur coordonne le pipeline : EditPlanner -> Writers paralleles (avec semaphore) -> Assemblage -> EditReviewer (optionnel, avec boucle de correction). Il enrichit le skeleton avec le texte actuel des elements (`CurrentText`) depuis les donnees de la presentation.

## Alternatives evaluees

### Conserver l'EditPlanner monolithique avec selection de modele dynamique

Garder un seul agent mais choisir le modele (Haiku/Sonnet/Opus) selon la complexite de la demande globale. Rejete car cela ne resout ni le manque de parallelisme ni l'absence de revue qualite. De plus, un modele leger comme Haiku peut suffire pour generer le texte d'un element simple mais pas pour les decisions structurelles qui necessitent le raisonnement de Sonnet.

### Etendre l'orchestrateur de creation existant

Ajouter un mode "edit" a `orchestrator.Orchestrator`. Rejete car les deux pipelines ont des etapes fondamentalement differentes (Outliner+Selector vs EditPlanner) et des types d'entree/sortie distincts. Partager le meme orchestrateur creerait un couplage artificiel.

### Pipeline unifie creation/edition

Fusionner les deux pipelines en un seul qui detecte automatiquement s'il s'agit d'une creation ou d'une edition. Rejete car la complexite supplémentaire n'est pas justifiee : les deux modes ont des points d'entree CLI distincts et des flux utilisateur differents.

## Consequences

### Positives

- **Cout optimise** : les operations simples utilisent Haiku au lieu de Sonnet, reduisant le cout et la latence
- **Parallelisme** : les operations independantes sont traitees en parallele, accelerant les editions multi-slides
- **Revue qualite** : un reviewer optionnel detecte les incoherences avant execution
- **Coherence architecturale** : le pipeline d'edition suit le meme pattern que le pipeline de creation (agents specialises, metriques, semaphore)
- **Feedback utilisateur plus rapide** : l'approbation sur le skeleton evite la generation de texte pendant les iterations

### Negatives

- **Complexite accrue** : trois nouveaux packages (`editwriter`, `editreviewer`, `editorchestrator`) au lieu d'un seul agent
- **Appels API supplementaires** : la decomposition planner + writers genere plus d'appels API que l'approche monolithique (partiellement compense par le parallelisme et l'utilisation de modeles moins chers)
- **Latence potentielle** : pour les editions tres simples (un seul champ texte), l'overhead de l'orchestration peut depasser le gain de parallelisme

## Fichiers concernes

| Fichier | Modification |
|---------|-------------|
| `internal/model/edit_skeleton.go` | Nouveau : types `EditSkeleton`, `SkeletonOperation`, `ModificationIntent`, `ContentIntent` |
| `internal/agent/config.go` | Ajout : `EditWriterModel`, `EditWriterSimpleModel`, `EditReviewerModel`, etc. |
| `internal/agent/editplanner/editplanner.go` | Modifie : retourne `EditSkeleton`, schema outil avec `intention` au lieu de `newText` |
| `internal/agent/editplanner/prompt_editplanner.txt` | Modifie : renforce la regle d'intentions sans texte |
| `internal/agent/editwriter/` | Nouveau package : agent de generation de contenu pour `modify_content` |
| `internal/agent/editreviewer/` | Nouveau package : agent de validation qualite pour les plans d'edition |
| `internal/agent/editorchestrator/` | Nouveau package : orchestrateur du pipeline d'edition |
| `slidegen/main.go` | Modifie : `editMode()` utilise `EditOrchestrator` |
| `docs/architecture.md` | Modifie : documentation du pipeline d'edition agentique |
