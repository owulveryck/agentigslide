# ADR 010 : Modification de presentations existantes

- **Date** : 2026-05-20
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le systeme genere des presentations a partir de zero en dupliquant des slides templates et en modifiant leur contenu textuel. Une fois la presentation generee, toute modification doit etre effectuee manuellement dans Google Slides. Le mode "amend" existant (`--plan`) modifie un plan JSON sauvegarde avant execution, mais ne peut pas operer sur une presentation deja creee.

Les utilisateurs ont besoin de pouvoir modifier des presentations existantes : changer le texte d'une slide, remplacer une slide par un autre template (quand le layout ne convient plus), ajouter ou supprimer des slides. Ces operations doivent etre pilotees par un agent LLM a partir d'une demande en langage naturel.

## Decision

### Mode CLI `--presentation <ID>`

Un nouveau flag `--presentation` dans slidegen entre en mode edition. Il est mutuellement exclusif avec `--plan`. Combinable avec `--file` pour une edition directe, ou seul pour le mode interactif (chat).

### Lecture de la presentation existante

Une fonction `ReadPresentation()` utilise `Presentations.Get()` pour extraire la structure de chaque slide : pageObjectID, elements texte (ObjectID, contenu, type de shape). Cela evite un appel Claude Vision pour la lecture structurelle — les donnees de l'API suffisent pour identifier les champs editables.

### Modifications de contenu in-place

Les changements de texte (`modify_content`) et les suppressions de slides (`delete_slide`) sont appliques directement sur la presentation existante via `BatchUpdate` (pattern `DeleteText` + `InsertMarkdownContent`). La presentation conserve son URL.

### Remplacement et insertion de slides via recreation d'elements

L'API Google Slides ne supporte pas `DuplicateObject` entre presentations differentes. Pour inserer une slide template dans une presentation existante ou remplacer une slide par un autre template, le systeme :

1. Lit la structure complete de la slide template source via `Presentations.Get(templatePresID)`
2. Cree une slide vierge dans la presentation cible (`CreateSlide`)
3. Recree programmatiquement chaque element de la slide template (`CreateShape`, `CreateImage`, `CreateTable`, `CreateLine`, `GroupObjects`) avec les memes proprietes (position, taille, remplissage, contour, texte)
4. Applique le contenu genere par le Writer sur les elements recrees

Ce choix est motive par l'absence d'alternative dans l'API et par le souhait de modifier in-place plutot que de regenerer une nouvelle presentation.

### Nouvel agent EditPlanner

Un agent dedie recoit la description structurelle de la presentation existante, la demande de modification de l'utilisateur, et le catalogue de templates. Il produit un `EditPlan` structure via tool_use, avec quatre types d'operations : `modify_content`, `replace_slide`, `insert_slide`, `delete_slide`. Cet agent remplace la chaine Outliner+Selector pour le flux d'edition.

## Alternatives evaluees

### Re-generation complete de la presentation

Analyser la presentation existante, reconstituer un `GenerationPlan` equivalent, appliquer les modifications, puis generer une nouvelle presentation via le pipeline `ExecutePlan` existant. Rejete car cela cree une nouvelle presentation a chaque modification (perte d'URL, duplication) et necessite un reverse-engineering fragile des slides existantes vers les templates.

### Utilisation de Google Apps Script pour la copie cross-presentation

Deployer un Apps Script comme web app pour copier des slides entre presentations (l'API Apps Script supporte `appendSlide()`). Rejete pour eviter la dependance externe et la complexite de deploiement/authentification supplementaire.

### Modification du plan JSON sauvegarde (mode amend existant)

Etendre le mode `--plan` pour lire la presentation reelle et mettre a jour le plan. Rejete car le plan JSON peut diverger de la presentation reelle (modifications manuelles, fixfonts) et ne represente pas l'etat courant.

### Lecture semantique via Claude Vision

Utiliser Claude Vision pour analyser visuellement chaque slide (comme `fixfonts` ou `analyzeSlides`). Rejete pour la lecture initiale car l'API structurelle suffit pour identifier les champs texte et leurs ObjectIDs. Claude Vision pourrait etre ajoute en complement futur pour une comprehension semantique plus riche.

## Consequences

### Positives

- **Modification in-place** : les modifications de contenu conservent l'URL et l'historique de la presentation
- **Coherence architecturale** : l'EditPlanner suit le meme pattern que les autres agents (tool_use, envconfig, metriques)
- **Reutilisation** : le Writer existant genere le contenu, le markdown rendering existant formate le texte
- **Extensible** : la fonction `ImportTemplateSlide` peut etre enrichie pour supporter des types d'elements supplementaires

### Negatives

- **Complexite de la recreation d'elements** : reproduire fidelement tous les types de PageElement (40+ types de shapes, groupes imbriques, tables avec bordures) est un effort significatif
- **Fidelite visuelle partielle** : certains elements complexes (formes custom, ombres, themes) peuvent ne pas etre reproduits parfaitement a la recreation
- **Pas de support master/layout** : les slides recreees utilisent un layout blank, sans heritage du master de la presentation template
- **Images temporaires** : les `contentUrl` des images dans l'API Slides sont des URLs temporaires, leur copie peut echouer si elles expirent

## Fichiers concernes

| Fichier | Modification |
|---------|-------------|
| `internal/model/edit.go` | Types `ExistingSlideInfo`, `ExistingText`, `EditPlan`, `EditOperation` |
| `internal/pipeline/read.go` | Fonction `ReadPresentation()` via `Presentations.Get()` |
| `internal/pipeline/edit.go` | Fonction `ExecuteEditPlan()` pour les operations in-place |
| `internal/pipeline/slideimport.go` | Fonction `ImportTemplateSlide()` pour la recreation d'elements |
| `internal/agent/editplanner/` | Nouveau package : `editplanner.go`, `prompt_editplanner.txt`, `embed.go` |
| `internal/agent/config.go` | `EditPlannerModel`, `EditPlannerMaxTokens` |
| `slidegen/main.go` | Flag `--presentation`, fonction `editMode()`, mise a jour `flag.Usage` |
| `docs/architecture.md` | Documentation du flux d'edition |
| `README.md` | Mise a jour des commandes CLI |
