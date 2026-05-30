# Glossaire / Langage Omnipresent (Ubiquitous Language)

Ce document definit le vocabulaire partage du projet AgentiGSlide. Il fait autorite sur la signification des termes utilises dans le code, la documentation et les echanges.

**Quand le consulter** : avant d'implementer une fonctionnalite, quand un terme semble ambigu, ou quand on introduit un nouveau concept.

**Comment le maintenir** : toute modification de concept (ajout d'agent, renommage de type, changement de phase) doit etre refletee ici.

---

## Concepts fondamentaux

### Presentation

Fichier Google Slides identifie par un ID unique. Deux usages dans le systeme :
- **Template** : presentation source contenant les slides preformatees OCTO, analysee une seule fois. On n'y touche jamais en ecriture.
- **Presentation generee** : copie du template modifiee par le pipeline pour produire le livrable final.

### Slide

Page individuelle dans une presentation Google Slides. Identifiee par un `pageObjectID` unique.

### Element

Composant d'une slide : forme (shape), image, tableau, groupe, ligne. Chaque element a un `ObjectID` unique.

### ObjectID

Identifiant unique attribue par Google Slides a chaque element d'une presentation (pages, formes, images, cellules de tableau). Chaine opaque (ex: `g344a0977514_44_0`). Lors de la duplication de slides, le systeme utilise un mapping predictible `d{compteur}_{IDoriginal}` pour controler les nouveaux ObjectIDs.

### EMU (English Metric Units)

Unite de mesure interne de Google Slides. 1 EMU = 1/914400 pouce. Conversion : 1 point = 12 700 EMU. Utilisee pour les positions, tailles et transformations de tous les elements.

### Placeholder

Type de placeholder Google Slides (`TITLE`, `BODY`, `SUBTITLE`, etc.). Indique le role semantique prevu d'un element dans le layout de la slide.

### SlideRef

Structure de mapping qui associe les ObjectIDs d'une slide template aux ObjectIDs de sa copie dans la presentation generee. Contient le `pageObjectID` de la copie et un `elementMap` (ancien ID -> nouvel ID).

### Template Index (`template_index.json`)

Catalogue cherchable de toutes les slides du template. Chaque entree (`TemplateSlide`) contient les metadonnees d'analyse (intention, categorie, tags d'usage, style visuel), les champs editables (`EditableFieldSummary`), et les elements visuels (`VisualElementSummary`). Produit une seule fois par la phase d'analyse.

### Compact Catalog

Representation textuelle compacte du Template Index, optimisee pour la consommation par les LLM (economie de tokens). Format texte structure listant pour chaque slide : numero, comptage de champs par role, intention, description, categorie, et detail des champs editables avec leurs limites de caracteres.

### Variable Name (Nom de variable)

Identifiant semantique en anglais camelCase attribue a chaque champ editable d'une slide template. Convention : `{role}{position}Shape`.

Exemples : `titleMainShape`, `bodyTextShape`, `yearBottomLeftShape`, `table2Shape`.

Generation : role infere depuis la description Claude + suffixe de position si plusieurs elements partagent le meme role + suffixe "Shape".

### Role

Classification semantique en francais snake_case d'un champ editable, inferee depuis l'analyse Claude Vision. Valeurs possibles : `titre_principal`, `sous_titre`, `corps_texte`, `annee`, `entreprise`, `tableau`, `citation`, `nom`, `poste`, `description`, `contact`, `numero`, `legende`.

Le role est traduit en prefixe anglais pour le nom de variable (ex: `titre_principal` -> `titleMain`).

### Categorie

Classification semantique d'une slide entiere. Valeurs : `couverture`, `intercalaire`, `contenu_texte`, `contenu_illustre`, `donnees_tableau`, `donnees_graphique`, `citation`, `equipe`, `timeline`, `diagramme`, `conclusion`, `question`.

### Style visuel

Apparence globale d'une slide. Valeurs : `minimal`, `illustre`, `data`, `pleine_image`, `split`.

### Tags d'usage (useCaseTags)

3 a 5 descripteurs en francais du cas d'utilisation d'une slide (ex: "presentation d'equipe", "staffing", "page de titre"). Utilises par le Selector pour le matching semantique.

---

## Les phases du systeme

### Phase 1 : Analyse du template

Executee **une seule fois** pour un template donne. Produit le Template Index.

| Etape | Description | Entree | Sortie |
|-------|------------|--------|--------|
| Extraction | Lecture de la structure via API Google Slides | ID du template | `content.json` par slide |
| Export images | Capture visuelle de chaque slide | API Google Slides | `slide.png` par slide |
| Analyse vision | Claude Vision identifie elements editables et visuels | `slide.png` + `content.json` | `analysis.json` par slide |
| Indexation | Agregation, inference de roles, noms de variables | Tous les `analysis.json` | `template_index.json` |

### Phase 2 : Planification (pipeline multi-agent)

Executee **a chaque demande**. Decompose la demande utilisateur en un plan de generation.

Flux : Outliner -> Selector -> Writers/Designer (parallele) -> Assembler -> Reviewer (boucle de feedback).

### Phase 3 : Production

Transforme le `PresentationPlan` en presentation Google Slides reelle.

| Etape | Description |
|-------|------------|
| Duplication template | `Drive.Files.Copy` cree une copie complete |
| Duplication slides | `DuplicateObject` duplique chaque slide selectionnee in-situ |
| Nettoyage | Suppression des slides originaux du template |
| Reordonnancement | `UpdateSlidesPosition` remet les slides dans l'ordre du plan |
| Modification texte | `BatchUpdate` applique les contenus (avec support markdown) |
| Creation diagrammes | `CreateSlide` + `CreateShape` + `CreateLine` pour les slides `diagram` |

### Phase 4 : Post-production (Formatter)

**Optionnelle**. Verifie la coherence du formatage (polices, couleurs, tailles, espacement, alignement) par analyse deterministe de la structure API Google Slides. Pas d'appel LLM.

### Phase 5 : Edition de presentations existantes

Modifie une presentation deja generee sans la recreer. Pipeline specifique : EditPlanner -> EditWriters/Writers -> EditReviewer (optionnel) -> Review visuelle -> Formatter.

---

## Agent -- Definition

Un **agent** est un composant logiciel autonome qui :
1. Recoit une **entree structuree** (texte ou JSON)
2. Invoque un **LLM** (Claude via Vertex AI) avec un prompt systeme dedie et un schema `tool_use`
3. Produit une **sortie JSON typee** via l'appel d'outil

Chaque agent :
- Vit dans son propre package Go sous `internal/agent/`
- A un **prompt systeme** externalise (fichier `.txt` embarque via `go:embed`)
- Definit un **schema d'outil** JSON Schema pour structurer la sortie du LLM
- Expose une **AgentCard** A2A et peut etre deploye comme serveur A2A standalone
- Utilise un modele Claude configurable (Sonnet, Haiku, ou Opus selon la complexite)

### Sous-agent

Terme informel designant un agent specialise qui opere sous la coordination d'un **orchestrateur**. Par exemple, les Writers et le Designer sont des sous-agents de l'Orchestrator : ils sont invoques en parallele par l'orchestrateur, qui collecte leurs resultats et gere les erreurs.

La distinction est organisationnelle, pas technique : un sous-agent a la meme structure qu'un agent, mais il n'est pas invoque directement par l'utilisateur.

### Orchestrateur

Agent coordinateur qui orchestre la sequence d'execution des sous-agents. Il ne fait pas d'appel LLM lui-meme (Go pur). Il gere le flux de donnees entre agents, la concurrence (goroutines), et les boucles de feedback.

Deux orchestrateurs existent :
- **Orchestrator** (`internal/agent/orchestrator/`) : pipeline de creation
- **EditOrchestrator** (`internal/agent/editorchestrator/`) : pipeline d'edition

---

## Agents du pipeline de creation

### Outliner

| | |
|---|---|
| **Package** | `internal/agent/outliner/` |
| **Role** | Analyse la demande utilisateur et produit un plan structure de presentation |
| **Modele par defaut** | Claude Sonnet 4.6 |
| **Outil** | `produce_outline` |
| **Entree** | Texte libre de l'utilisateur (demande de presentation) |
| **Sortie** | `PresentationOutline` (titre + sections + SlideNeeds) |

L'Outliner ne connait **pas** les templates disponibles. Il travaille uniquement sur la structure du contenu demande. Regle fondamentale : ne jamais inventer de contenu absent de la demande.

En mode interactif, l'Outliner supporte un dialogue multi-tour pour raffiner le plan avant de lancer le reste du pipeline.

### Selector

| | |
|---|---|
| **Package** | `internal/agent/selector/` |
| **Role** | Mappe chaque SlideNeed a la meilleure slide template |
| **Modele par defaut** | Claude Sonnet 4.6 |
| **Outil** | `select_templates` |
| **Entree** | `PresentationOutline` + Compact Catalog |
| **Sortie** | `SelectionPlan` (liste de SlideSelection) |

Le Selector choisit les templates en fonction de la correspondance semantique (tags d'usage, categorie), de la capacite (nombre de champs, `maxChars`), du type de slide, et de la coherence visuelle.

**Contrainte globale** : tous les `section_divider` doivent utiliser le meme template (validee par `validateSelectionGlobal()`).

Retries automatiques (jusqu'a `AGENT_MAX_SELECTOR_RETRIES`) en cas d'echec de validation, avec feedback des erreurs.

### Writer

| | |
|---|---|
| **Package** | `internal/agent/writer/` |
| **Role** | Genere le contenu textuel pour une slide |
| **Modele par defaut** | Sonnet 4.6 (complexe, >2 champs) / Haiku 4.5 (simple, <=2 champs) |
| **Outil** | `produce_slide_content` (schema dynamique adapte aux champs de la slide) |
| **Entree** | `SlideNeed` + liste de `TemplateField` + feedback Reviewer (optionnel) |
| **Sortie** | `SlideContent` (liste de `TextModification`) |

Les Writers s'executent **en parallele** (jusqu'a `AGENT_MAX_PARALLEL` goroutines).

Support markdown : `**gras**`, `*italique*`, `` `code` `` (Courier New), listes a puces.

Post-traitement : `enforceMaxChars()` tronque intelligemment le texte genere pour respecter les limites de taille.

### Designer

| | |
|---|---|
| **Package** | `internal/agent/designer/` |
| **Role** | Cree la topologie d'un diagramme pour les slides de type `diagram` |
| **Modele par defaut** | Claude Sonnet 4.6 |
| **Outil** | `design_diagram` |
| **Entree** | `SlideNeed` (intent + content items) + feedback (optionnel) |
| **Sortie** | `DiagramSpec` (noeuds, aretes, groupes, direction de layout) |

Le Designer ne calcule pas les coordonnees. Il produit une topologie abstraite. Le **layout** (positionnement en EMU) est calcule par un algorithme Go en couches (Sugiyama simplifie) dans `internal/diagram/layout.go`. Le **rendu** (conversion en requetes API Google Slides) est fait par `internal/diagram/render.go`.

### Assembler

| | |
|---|---|
| **Package** | `internal/agent/orchestrator/` (integre a l'orchestrateur) |
| **Role** | Combine les sorties de tous les Writers et Designers en un plan unifie |
| **Modele** | Aucun (Go pur, pas d'appel LLM) |
| **Entree** | Tableau de `SlideContent` + map de `DiagramSpec` |
| **Sortie** | `GenerationPlan` |

### Reviewer

| | |
|---|---|
| **Package** | `internal/agent/reviewer/` |
| **Role** | Valide la qualite du plan assemble |
| **Modele par defaut** | Claude Opus 4.6 (avec extended thinking) |
| **Outil** | `submit_review` |
| **Entree** | `GenerationPlan` + demande utilisateur + Compact Catalog |
| **Sortie** | `ReviewResult` (approuve ou liste de `ReviewIssue`) |

Types d'anomalies detectees :
- `overflow` : contenu depassant la capacite du template
- `duplicate` : contenu repete entre slides
- `missing_content` : elements de la demande absents
- `wrong_template` : slide mal adaptee au contenu
- `incoherence` : contradictions entre slides
- `invented_content` : information absente de la demande originale
- `diagram_topology` : erreur dans la structure du diagramme

**Boucle de feedback** : si non approuve, les issues sont renvoyees aux Writers concernes (max `AGENT_MAX_REVIEW_RETRIES`). Seules les slides corrigees sont re-validees.

### Formatter

| | |
|---|---|
| **Package** | `internal/agent/formatter/` |
| **Role** | Verifie la coherence du formatage et applique des corrections deterministes |
| **Modele par defaut** | Aucun (agent deterministe, pas d'appel LLM) |
| **Outil** | N/A |
| **Entree** | Presentation Google Slides (via `Presentations.Get`) |
| **Sortie** | `FormatterResult` (issues de coherence + corrections appliquees) |

Le Formatter est un agent deterministe qui ne fait aucun appel LLM. Il extrait la structure complete de la presentation, applique 9 regles de coherence (police par role, taille par role, hierarchie de tailles, palette de couleurs, fond par role, espacement, alignement, emphase, outline), et corrige les incoherences en utilisant le vote majoritaire comme reference.

### Orchestrator

| | |
|---|---|
| **Package** | `internal/agent/orchestrator/` |
| **Role** | Coordonne le pipeline de creation complet |
| **Modele** | Aucun (Go pur) |

Sequence : Outliner -> Selector -> Writers/Designer (parallele) -> Assembler -> Reviewer (boucle).

Gere le `PipelineState` (etat mutable partage, thread-safe via mutex).

---

## Agents du pipeline d'edition

### EditPlanner

| | |
|---|---|
| **Package** | `internal/agent/editplanner/` |
| **Role** | Analyse la presentation existante et la demande de modification, produit les decisions structurelles |
| **Modele par defaut** | Claude Opus 4.6 |
| **Entree** | Slides existantes (`[]ExistingSlideInfo`) + demande utilisateur + Compact Catalog |
| **Sortie** | `EditSkeleton` (operations avec **intentions**, pas de texte final) |

Operations possibles :
- `modify_content` : modifier le texte sans changer le layout
- `delete_slide` : supprimer une slide
- `replace_slide` : remplacer par un autre template
- `insert_slide` : ajouter une slide depuis le catalogue

En mode interactif, l'utilisateur affine le skeleton (intentions) avant la generation de texte.

### EditWriter

| | |
|---|---|
| **Package** | `internal/agent/editwriter/` |
| **Role** | Genere le texte final pour les operations `modify_content` |
| **Modele par defaut** | Sonnet 4.6 (complexe) / Haiku 4.5 (simple) |
| **Entree** | Liste de `ModificationIntent` (texte actuel + intention) |
| **Sortie** | Liste de `TextModification` (texte final) |

### EditReviewer

| | |
|---|---|
| **Package** | `internal/agent/editreviewer/` |
| **Role** | Valide la fidelite du texte genere aux intentions du skeleton |
| **Modele par defaut** | Claude Opus 4.6 |
| **Activation** | Desactive par defaut (`AGENT_EDIT_REVIEW_ENABLED=false`) |

Types d'anomalies : `intention_mismatch`, `coherence_break`, `over_modification`, `quality_issue`, `missing_content`.

### EditOrchestrator

| | |
|---|---|
| **Package** | `internal/agent/editorchestrator/` |
| **Role** | Coordonne le pipeline d'edition complet |

Sequence : EditPlanner -> EditWriters/Writers (parallele) -> Assembleur -> EditReviewer (optionnel) -> Execution API -> Review visuelle (optionnel) -> Formatter (optionnel).

---

## Types de donnees cles

### Structure de l'outline

| Type | Definition | Package |
|------|-----------|---------|
| `PresentationOutline` | Plan structure : titre + sections ordonnees | `internal/agent/types.go` |
| `SectionSpec` | Section logique : titre, objectif, liste de SlideNeeds | `internal/agent/types.go` |
| `SlideNeed` | Ce qu'une slide doit transmettre : intent, items de contenu, type de slide | `internal/agent/types.go` |

Types de slides (`SlideNeed.SlideType`) : `cover`, `section_divider`, `content`, `data`, `diagram`, `conclusion`.

### Selection

| Type | Definition | Package |
|------|-----------|---------|
| `SelectionPlan` | Resultat du Selector : liste de selections | `internal/agent/types.go` |
| `SlideSelection` | Mapping d'un SlideNeed (par index) vers un template (par numero de slide source) | `internal/agent/types.go` |
| `TemplateField` | Champ editable d'un template : variableName, role, maxChars | `internal/agent/types.go` |

### Contenu genere

| Type | Definition | Package |
|------|-----------|---------|
| `SlideContent` | Contenu d'une slide : numero source + modifications | `internal/agent/types.go` |
| `TextModification` | Mapping variableName -> nouveau texte | `internal/agent/types.go` |

### Diagrammes

| Type | Definition | Package |
|------|-----------|---------|
| `DiagramSpec` | Topologie d'un diagramme : titre, direction de layout, noeuds, aretes, groupes | `internal/agent/types.go`, `internal/model/plan.go` |
| `DiagramNode` | Noeud : id, label, forme (rectangle, ellipse, diamond), style | idem |
| `DiagramEdge` | Arete : from, to, label, style de ligne (arrow, dashed_arrow, line) | idem |
| `DiagramGroup` | Zone visuelle regroupant des noeuds : id, label, liste de noeuds | idem |

### Review

| Type | Definition | Package |
|------|-----------|---------|
| `ReviewResult` | Resultat d'une review : approuve (boolean) + liste d'issues | `internal/agent/types.go` |
| `ReviewIssue` | Anomalie : slideIndex, field, issueType, description, suggestion | `internal/agent/types.go` |

### Plans de generation

| Type | Definition | Package |
|------|-----------|---------|
| `GenerationPlan` | Sortie brute Claude : titre + liste de SlideRequest | `internal/model/plan.go` |
| `SlideRequest` | Slide dans un GenerationPlan : sourceSlide + modifications + diagram (optionnel) | `internal/model/plan.go` |
| `PresentationPlan` | Plan enrichi pret a l'execution : metadonnees completes, SlideSpecs | `internal/model/plan.go` |
| `SlideSpec` | Slide dans un PresentationPlan : position, source, elements editables/visuels, diagram | `internal/model/plan.go` |
| `EditableObject` | Champ editable enrichi : ObjectID, variableName, role, valeur actuelle/nouvelle | `internal/model/plan.go` |

### Edition

| Type | Definition | Package |
|------|-----------|---------|
| `EditSkeleton` | Plan d'edition structurel : intentions sans texte final | `internal/model/edit_skeleton.go` |
| `SkeletonOperation` | Operation d'edition avec intentions (type, slideIndex, rationale) | `internal/model/edit_skeleton.go` |
| `ModificationIntent` | Intention de modification : variableName, texte actuel, intention | `internal/model/edit_skeleton.go` |
| `ContentIntent` | Intention de contenu pour slide nouvelle/remplacee | `internal/model/edit_skeleton.go` |
| `EditPlan` | Plan d'edition avec texte final, pret a l'execution | `internal/model/edit.go` |
| `EditOperation` | Operation d'edition avec texte final | `internal/model/edit.go` |
| `ExistingSlideInfo` | Etat actuel d'une slide existante (index, pageObjectID, elements texte) | `internal/model/edit.go` |

### Analyse et template

| Type | Definition | Package |
|------|-----------|---------|
| `SlideAnalysis` | Resultat Claude Vision pour une slide : intention, elements, classifications | `internal/model/analysis.go` |
| `EditableElement` | Champ texte identifie par Claude (objectId, type, placeholder, description) | `internal/model/analysis.go` |
| `VisualElement` | Composant visuel (image, icone, logo) avec indicateur reutilisable | `internal/model/analysis.go` |
| `TemplateIndex` | Index complet du template : templateId + liste de TemplateSlide | `internal/model/template.go` |
| `TemplateSlide` | Metadonnees d'une slide template (intention, categorie, champs, visuels) | `internal/model/template.go` |

---

## Protocole A2A (Agent-to-Agent)

Protocole de communication inter-agents defini par Google (`github.com/a2aproject/a2a-go/v2`). Chaque agent implemente l'interface `a2asrv.AgentExecutor`.

| Concept | Definition |
|---------|-----------|
| `AgentCard` | Carte d'identite d'un agent : nom, description, version, competences (skills). Exposee via `/.well-known/agent-card.json` |
| `ExecuteA2A` | Helper generique (`internal/agent/a2ahelper.go`) qui orchestre le flux : soumission -> extraction entree -> execution -> emission artefact -> completion |
| `ExtractDataInput` | Extraction d'une entree JSON typee depuis les parties `data` d'un message A2A |
| `ExtractTextInput` | Extraction de texte concatene depuis les parties `text` d'un message A2A |
| Artefact | Sortie JSON d'un agent, emise comme piece jointe du resultat A2A |

---

## Operations API Google Slides

| Operation | Description |
|-----------|------------|
| `DuplicateObject` | Duplique une slide (et tous ses elements) a l'interieur d'une meme presentation. Ne fonctionne pas entre presentations differentes. |
| `DeleteObject` | Supprime un element ou une slide entiere |
| `BatchUpdate` | Appel atomique regroupant plusieurs modifications en une seule requete |
| `InsertText` | Insere du texte dans un element |
| `DeleteText` | Supprime le texte existant d'un element |
| `UpdateTextStyle` | Applique gras, italique, police, taille sur une plage de texte |
| `CreateParagraphBullets` | Convertit des lignes en liste a puces |
| `UpdateSlidesPosition` | Reordonne les slides dans la presentation |
| `CreateSlide` | Cree une slide vierge (layout BLANK) |
| `CreateShape` | Cree une forme (rectangle, ellipse, etc.) |
| `CreateLine` | Cree une connexion (fleche, ligne) entre elements |
| `ImportTemplateSlide` | Fonction du systeme (pas de l'API Google) qui recree programmatiquement une slide template dans une autre presentation, contournant la limitation de `DuplicateObject` cross-presentation |

---

## Modes d'execution

| Mode | Commande | Description |
|------|----------|------------|
| **Creation interactive** | `slidegen` | Mode par defaut. Chat multi-tour pour raffiner l'outline, puis pipeline complet |
| **Creation directe** | `slidegen --file request.md` | Pipeline complet sans interaction, depuis un fichier markdown |
| **Edition interactive** | `slidegen --presentation <ID>` | Modification d'une presentation existante avec raffinement du skeleton |
| **Edition directe** | `slidegen --presentation <ID> --file edits.md` | Modification directe depuis un fichier |
| **Amend** | `slidegen --plan plan.json --file request.md` | Modification d'un plan de generation existant |
| **Recovery** | `slidegen --plan plan.json` | Re-execution d'un plan sauvegarde |
| **Web dashboard** | `slidegen --web` | Dashboard temps reel de l'avancement du pipeline |
| **A2A serveur** | `orchestrator` | Expose le pipeline comme serveur A2A REST |
| **MCP serveur** | `exp/mcp-server` | Expose le pipeline comme outil MCP `generate_slides` |

---

## Vertex AI

Toutes les interactions avec Claude passent par **Google Cloud Vertex AI** (jamais par l'API Anthropic directement).

| Concept | Definition |
|---------|-----------|
| `vertex.Client` | Client HTTP encapsulant l'authentification ADC et le retry exponentiel |
| `Message` | Message de conversation (role user/assistant, contenu multi-bloc) |
| `ContentBlock` | Bloc de contenu : texte, image base64, document PDF, tool_use, tool_result |
| `Tool` | Definition d'outil (nom, description, JSON Schema) pour `tool_use` |
| `ThinkingConfig` | Configuration de l'extended thinking (budget en tokens) |
| Prompt caching | Mecanisme Vertex AI (`cache_control: {"type": "ephemeral"}`) pour reutiliser le prefixe de prompt entre appels paralleles |

---

## Autres composants

### Formatter (`internal/agent/formatter/`)

Agent deterministe de verification de coherence du formatage. Processus en 4 etapes :
1. Extraction enrichie de la structure (polices, couleurs, tailles, espacement, alignement, outline) via Slides API
2. Verification de coherence via 9 regles deterministes (vote majoritaire par role)
3. Generation de corrections pour les elements non conformes
4. Application des corrections via `BatchUpdate`

Types de corrections (`Correction`) : `textStyle` (police, taille, couleur, bold/italic), `paragraphStyle` (interligne, espacement, alignement), `shapeProperties` (fond, alignement interne, outline).

Pas d'appel LLM. Pas d'export PDF.

### Revision Log (`internal/revision/`)

Suivi de toutes les operations `BatchUpdate` appliquees a une presentation. Chaque `Entry` enregistre le nom de l'operation, l'ID de revision Google Slides, et le timestamp. Permet l'audit et le suivi de la production.

### Markdown

Support d'un sous-ensemble de markdown dans les contenus textuels : `**gras**`, `*italique*`, `` `code` `` (rendu en Courier New), listes a puces (un ou deux niveaux). Utilise la bibliotheque `goldmark` pour le parsing AST, puis traduit en requetes API Google Slides.
