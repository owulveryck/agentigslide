# ADR 012 : Post-traitement visuel et correction de formatage apres edition

- **Date** : 2026-05-20
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline d'edition (ADR 011) s'arrete apres l'execution du `EditPlan` via `ExecuteEditPlan()`. Contrairement au pipeline de creation qui dispose d'une etape `fixfonts` pour corriger les problemes de formatage (polices, tailles, espacement), le mode edition n'a aucun post-traitement visuel.

Cela pose deux problemes :

1. **Pas de verification visuelle** : apres modification du contenu textuel, il est possible que le texte deborde de sa zone, soit tronque, ou que le layout soit casse. Aucun controle automatique ne detecte ces problemes.

2. **Pas de correction de formatage** : les operations `DeleteText` + `InsertText` utilisees pour modifier le contenu peuvent alterer le formatage (taille de police, espacement). Le pipeline de creation corrige cela via `fixfonts`, mais cette etape n'existe pas en edition.

Le pipeline de creation possede deja l'infrastructure necessaire :
- `fixfonts.Run()` pour l'analyse PDF + correction de formatage via Claude Vision
- `ReviewDiagramVisual()` pour la review visuelle per-slide via thumbnails

## Decision

### Tracking des slides modifies via EditResult

`ExecuteEditPlan()` retourne un nouveau type `EditResult` contenant les `PageObjectID`s de toutes les slides affectees (creees ou modifiees). Les slides supprimees sont exclues.

Les IDs sont captures pendant l'execution :
- `modify_content` : le `PageObjectID` de la slide a l'index cible
- `replace_slide` : le `newPageID` de la slide importee (l'originale est supprimee)
- `insert_slide` : le `newPageID` de la slide importee

Ce tracking pendant l'execution est prefere a une re-lecture post-execution car il est plus simple, plus rapide, et evite un appel API supplementaire.

### Review visuelle per-slide via thumbnails

Pour chaque slide affecte, un thumbnail PNG est exporte via `GetThumbnail()` et envoye a Claude Vision pour detection de problemes visuels :
- Texte qui deborde ou est tronque
- Champs de texte vides
- Desalignement d'elements
- Problemes de police ou de taille

Cette etape est informative en v1 : les problemes sont logges mais aucune correction automatique n'est appliquee. Le pattern reutilise celui de `ReviewDiagramVisual` (thumbnails + tool-use).

Les reviews sont executees en parallele (bornees par `MaxParallel`).

### Fixfonts cible via filtrage de structure

Le pipeline `fixfonts` existant est reutilise avec un filtrage :

1. Le PDF complet est exporte (pas d'API Drive pour exporter un sous-ensemble)
2. La structure n'est extraite que pour les slides cibles (`ExtractStructureForPages`)
3. L'analyse Claude recoit le PDF complet mais la structure JSON filtrée focalise l'attention
4. `ValidateCorrections` filtre naturellement les corrections pour des ObjectIDs hors scope

Cette approche maximise la reutilisation du code existant tout en ciblant l'effort d'analyse.

### Integration dans editMode()

Le post-traitement est integre dans `editMode()` apres `ExecuteEditPlan()`, pas dans l'orchestrateur. Cela maintient la separation des responsabilites :
- Orchestrateur : planification et generation de contenu (LLM)
- ExecuteEditPlan : mutations via Google Slides API
- Post-traitement : verification qualite visuelle

## Alternatives evaluees

### Review visuelle via PDF au lieu de thumbnails

Exporter le PDF complet et envoyer les pages concernees a Claude. Rejete : plus lent et plus couteux qu'un thumbnail per-slide, et le pattern thumbnail est deja prouve dans `ReviewDiagramVisual`.

### Fixfonts via thumbnails per-slide au lieu de PDF

Utiliser des thumbnails au lieu du PDF pour l'analyse fixfonts. Rejete : le PDF offre un rendu plus fidele que les thumbnails (resolution, fidelite typographique), et le pipeline fixfonts existant est concu pour le PDF.

### Boucle de correction automatique basee sur la review visuelle

Implementee en v1.1 : lorsque la review visuelle detecte des problemes `text_overflow` ou `text_truncated` sur des slides `modify_content`, le pipeline re-invoque les EditWriters avec un feedback demandant de raccourcir le texte, re-applique les modifications via `ReapplyModifications`, puis re-review visuellement. Le nombre d'iterations est borne par `AGENT_MAX_EDIT_VISUAL_RETRIES` (default: 1). Seuls les `modify_content` sont eligibles ; les `replace_slide`/`insert_slide` ne peuvent pas etre corriges par simple re-ecriture.

### Tracking via re-lecture post-execution

Re-lire la presentation apres execution et comparer avec l'etat initial pour deduire les slides modifies. Rejete : plus complexe, plus lent (appel API supplementaire), et inutile quand on peut tracker pendant l'execution.

## Consequences

### Positives

- **Detection automatique** des problemes visuels apres edition (overflow, troncature)
- **Correction de formatage** ciblee sur les slides modifies (pas toute la presentation)
- **Reutilisation maximale** du code existant (fixfonts, pattern thumbnail)
- **Cout proportionnel** au nombre de modifications (pas au nombre total de slides)

### Negatives

- **Latence supplementaire** : deux etapes de post-traitement ajoutent du temps (export PDF, appels Claude Vision)
- **Cout API** : chaque slide modifie genere un appel Vision pour la review + un appel fixfonts partage
- **Thumbnails potentiellement stales** : Google Slides peut mettre quelques secondes a rendre les modifications, risque de reviewer un etat pre-edit

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_EDIT_VISUAL_REVIEW_ENABLED` | `true` | Active la review visuelle post-edition |
| `AGENT_EDIT_VISUAL_REVIEW_MODEL` | `claude-sonnet-4-6` | Modele pour la review visuelle |
| `AGENT_MAX_EDIT_VISUAL_RETRIES` | `1` | Max iterations de feedback visuel (0 = review seule sans correction) |
| `AGENT_EDIT_FIXFONTS_ENABLED` | `true` | Active fixfonts sur les slides modifies |
| `FIXFONTS_MODEL` | `claude-opus-4-6` | (existant) Modele pour l'analyse fixfonts |

## Fichiers concernes

| Fichier | Modification |
|---------|-------------|
| `internal/pipeline/edit.go` | Modifie : `EditResult` avec `PageIDToOpIndex`, `ExecuteEditPlan` retourne `(*EditResult, error)`, ajout `ReapplyModifications()` |
| `internal/fixfonts/fixfonts.go` | Ajout : `ExtractStructureForPages()`, `RunForSlides()` |
| `internal/pipeline/edit_visual_review.go` | Nouveau : review visuelle per-slide avec retry backoff |
| `internal/agent/config.go` | Ajout : `EditVisualReviewEnabled`, `EditVisualReviewModel`, `EditFixfontsEnabled`, `MaxEditVisualRetries` |
| `internal/agent/editorchestrator/editorchestrator.go` | Ajout : `HandleVisualFeedback()`, `FinalSkeleton` |
| `slidegen/main.go` | Modifie : `editMode()` avec boucle de feedback visuel + post-traitement |
