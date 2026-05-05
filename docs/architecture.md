# Système de génération automatique de présentations Google Slides

Ce système permet de créer des présentations Google Slides complètes à partir d'une simple demande textuelle (ou markdown). Il s'appuie sur un template de slides préformatées OCTO qu'il analyse une fois avec une IA de vision, puis qu'il réutilise à la demande pour assembler et personnaliser des présentations.

Le processus se décompose en trois phases principales, suivies d'une phase optionnelle de post-production :

1. **Analyse** — extraction et compréhension du template (exécutée une seule fois)
2. **Planification** — choix des slides et du contenu par une IA générative (à chaque demande)
3. **Production** — duplication du template et application des modifications via les API Google (à chaque demande)
4. **Post-production** *(optionnelle)* — correction automatique du formatage par IA (polices, tailles, espacements)

## Vue d'ensemble

```
PHASE 1 : ANALYSE (une seule fois)     PHASE 2 : PLANIFICATION          PHASE 3 : PRODUCTION
                                        (à chaque demande)               (à chaque demande)

 Google Slides API                      Demande utilisateur              Google Drive API
        |                                       |                               |
        v                                       v                               v
 +--------------+                       +----------------+               +--------------+
 | Extraction   |                       | Chargement de  |               | Copie du     |
 | des données  |                       | template_index |               | template     |
 | brutes       |                       |    .json       |               | (Drive.Copy) |
 |              |                       +-------+--------+               +------+-------+
 | content.json |                               |                              |
 | slide.png    |                               v                              v
 +------+-------+                       +----------------+               +--------------+
        |                               | Claude Sonnet  |               | Duplication  |
        v                               | Sélection des  |               | in-situ des  |
 +--------------+                       | slides +       |               | slides       |
 | Claude Opus  |                       | remplissage    |               | choisies     |
 | Vision       |                       +-------+--------+               +------+-------+
 | (Vertex AI)  |                               |                              |
 |              |                               v                              v
 | analysis.json|                       +----------------+               +--------------+
 +------+-------+                       | Enrichissement |               | Suppression  |
        |                               | avec metadata  |               | des originaux|
        v                               +-------+--------+               | + Réordonnc. |
 +--------------+                               |                       +------+-------+
 | Construction |                               v                              |
 | de l'index   |                       +----------------+                     v
 |              |                       | PresentationPlan               +--------------+
 | template_    |                       |    (JSON)      |               | Modification |
 | index.json   |                       +----------------+               | batch des    |
 +--------------+                                                        | textes       |
                                                                         +------+-------+
                                                                                |
                                                                                v
                                                                         Présentation
                                                                         finale (URL)
                                                                                |
                                                                         (optionnel)
                                                                                |
                                                                                v

PHASE 4 : POST-PRODUCTION (optionnelle)

 Google Drive API          Google Slides API
        |                         |
        v                         v
 +--------------+          +--------------+
 | Export PDF   |          | Extraction   |
 | de la        |          | structure    |
 | présentation |          | (polices,    |
 +--------------+          | tailles,     |
        |                  | espacements) |
        |                  +------+-------+
        |                         |
        +------------+------------+
                     |
                     v
              +--------------+
              | Claude Opus  |
              | (Vertex AI)  |
              | Détection    |
              | problèmes   |
              | formatage    |
              +------+-------+
                     |
                     v
              +--------------+
              | BatchUpdate  |
              | Corrections  |
              | (TextStyle + |
              | ParagraphSt.)|
              +--------------+
                     |
                     v
              Présentation
              corrigée (URL)
```

---

## Phase 1 : Analyse du template

Cette phase est exécutée **une seule fois** pour un template donné. Elle produit un index cherchable (`template_index.json`) qui sera utilisé par toutes les générations futures.

### Étape 1.1 — Extraction des données brutes depuis Google Slides API

Le programme `analysis/main.go` se connecte à l'API Google Slides en lecture seule et récupère la structure complète de la présentation template via `Presentations.Get(presentationID)`.

Pour chaque slide, il sauvegarde la réponse brute de l'API dans un fichier :

```
template/{presentationID}/{numéroSlide}/content.json
```

Ce fichier contient toute la structure de la slide telle que Google la voit :
- **ObjectIDs** : identifiants uniques de chaque élément (formes, images, groupes)
- **Positions et tailles** : en EMU (English Metric Units, 1 EMU = 1/914400 pouce)
- **Contenu textuel** : texte de chaque forme, avec styles
- **Type de placeholder** : TITLE, BODY, SUBTITLE, etc.

### Étape 1.2 — Extraction des images

Chaque slide est également exportée en image :

```
template/{presentationID}/{numéroSlide}/slide.png
```

Ces images servent d'entrée visuelle pour l'étape d'analyse par IA.

### Étape 1.3 — Analyse par IA vision (Claude Opus via Vertex AI)

Le programme `analyzeSlides/analyze_slides.go` envoie, pour chaque slide, deux éléments à Claude Opus 4.5 via l'API Vertex AI :

1. L'image de la slide (`slide.png`, encodée en base64)
2. Un résumé textuel extrait de `content.json` listant tous les objets avec leurs ObjectIDs et positions

```
slide.png --------+
                  +----> Claude Opus 4.5 ----> analysis.json
content.json --+  |      (Vertex AI)           analysis.md
  (résumé)  ---+--+
```

Claude identifie deux types d'éléments :

- **editableElements** : les champs de texte modifiables (titre, sous-titre, corps de texte, année...), chacun associé à son ObjectID issu de `content.json`
- **visualElements** : les éléments visuels réutilisables (icônes, images, logos) avec leurs ObjectIDs quand ils sont de type IMAGE ou GROUP

La sortie `analysis.json` est structurée ainsi :

```json
{
  "slideNumber": 1,
  "slideId": "g344a0977514_44_0",
  "intention": "Slide de couverture",
  "description": "Page de titre avec photo de fond et formes géométriques...",
  "editableElements": [
    {
      "objectId": "g3b4521dbf06_4_0",
      "type": "text",
      "placeholder": null,
      "content": "Slides préformatées",
      "description": "Titre principal de la slide",
      "location": "Centre-gauche, dans une forme capsule"
    }
  ],
  "visualElements": [
    {
      "objectId": "g3bb7b487657_9_4",
      "type": "icon",
      "description": "Icône décorative",
      "purpose": "Élément visuel de la charte OCTO",
      "reusable": true
    }
  ]
}
```

### Étape 1.4 — Construction de la représentation intermédiaire descriptive

Le programme `buildTemplateIndex/build_template_index.go` agrège tous les fichiers `analysis.json` en un unique `template_index.json`.

Pour chaque slide, il :
- **Extrait des mots-clés** (tokenisation + filtrage des mots vides français)
- **Infère un rôle sémantique** pour chaque élément éditable (ex: "titre principal" → `titre_principal`, "année" → `annee`)
- **Génère des noms de variables** sémantiques : rôle + suffixe de position si nécessaire + "Shape" (ex: `titleMainShape`, `yearBottomLeftShape`)
- **Charge les positions** depuis `content.json` pour désambiguïser les éléments de même rôle

Le résultat est le **catalogue cherchable complet du template** :

```json
{
  "templateId": "YOUR_TEMPLATE_PRESENTATION_ID",
  "slides": [
    {
      "slideNumber": 1,
      "slideId": "g344a0977514_44_0",
      "intention": "Slide de couverture",
      "keywords": ["couverture", "digital", "octo", "titre"],
      "editableFields": [
        {
          "objectId": "g3b4521dbf06_4_0",
          "role": "titre_principal",
          "content": "Slides préformatées",
          "variableName": "titlemainShape",
          "updateFunction": "updateTitlemainShape"
        }
      ],
      "visualElements": [...]
    }
  ]
}
```

C'est cette représentation intermédiaire qui fait le pont entre la structure brute des objets Google Slides et une description sémantique compréhensible par une IA générative.

---

## Phase 2 : Planification — choix des slides et du contenu

Cette phase est exécutée **à chaque demande** de génération de présentation. Elle utilise Claude Sonnet pour transformer une demande utilisateur en un plan structuré.

### Étape 2.1 — Traitement de la demande utilisateur

Le programme `generateSlideList/generate_slide_list.go` (ou `slidegen/main.go` pour le flux tout-en-un) reçoit la demande sous forme de texte libre ou de fichier markdown.

Il charge `template_index.json` et en construit une **version compacte** adaptée au prompt : pour chaque slide, le numéro, l'intention, les mots-clés, et la liste des champs éditables avec leurs noms de variables et leur contenu actuel.

### Étape 2.2 — Sélection par Claude Sonnet (Vertex AI)

Le programme envoie à Claude Sonnet 4.5 via Vertex AI :
- L'**index compact** du template (quelles slides existent, avec quels champs)
- La **demande utilisateur** (le contenu souhaité pour la présentation)
- Des **instructions** : ne pas inventer d'information, n'utiliser que le contenu fourni par l'utilisateur, choisir des slides dont la structure correspond au contenu disponible

Claude choisit les slides à utiliser, leur ordre, et le texte à insérer dans chaque champ éditable. Il retourne un JSON :

```json
{
  "presentationTitle": "Proposition d'Intervention",
  "slides": [
    {
      "sourceSlide": 1,
      "modifications": [
        {
          "variableName": "titlemainShape",
          "newText": "Proposition d'Intervention"
        }
      ]
    },
    {
      "sourceSlide": 5,
      "modifications": [
        {
          "variableName": "titlemainShape",
          "newText": "Notre **approche**"
        },
        {
          "variableName": "bodyShape",
          "newText": "- Point clé numéro un\n- Point clé numéro deux\n  - Sous-point détaillé"
        }
      ]
    }
  ]
}
```

Le texte supporte un sous-ensemble de **markdown** : `**gras**`, `*italique*`, et listes à puces avec indentation (tirets `-`).

### Étape 2.3 — Enrichissement du plan

Le plan brut retourné par Claude est enrichi avec les métadonnées complètes issues des fichiers `analysis.json` de chaque slide sélectionnée : ObjectIDs, descriptions, localisations, valeurs actuelles vs. nouvelles.

Le résultat est un `PresentationPlan` JSON complet, prêt à être exécuté :

```
                  template_index.json
                         |
                    (version compacte)
                         |
   Demande          +----v----+
   utilisateur ---->| Claude  |----> Plan brut (JSON)
                    | Sonnet  |           |
                    +---------+           v
                                   +-------------+
                   analysis.json ->| Enrichissem. |--> PresentationPlan
                   (par slide)     +-------------+        (complet)
```

---

## Phase 3 : Application du plan — mise en production

Cette phase transforme le `PresentationPlan` en une vraie présentation Google Slides via les API Google Drive et Slides.

### Étape 3.1 — Duplication du template via Google Drive API

Le programme `applySlideList/apply_slide_list.go` (ou `slidegen/main.go`) appelle `Drive.Files.Copy(templateID)` pour créer une **copie complète** de la présentation template (avec ses ~325 slides). Cette copie reçoit le titre choisi par Claude et un nouvel ID de présentation.

### Étape 3.2 — Duplication in-situ des slides choisies

Pour chaque slide du plan, le programme appelle l'API `DuplicateObject` sur la copie. Cet appel duplique une slide **à l'intérieur de la même présentation**, à côté de son original.

Le point critique : Google Slides génère de **nouveaux ObjectIDs** lors de toute duplication. Pour garder le contrôle, le programme utilise un mapping personnalisé d'IDs :

```
Original : g344a0977514_44_0         →  Copie : d1_g344a0977514_44_0
Élément  : g3b4521dbf06_4_0          →  Copie : d1_g3b4521dbf06_4_0
```

Le pattern `d{compteur}_{IDoriginal}` rend les IDs des copies **prédictibles**. Le mapping est suivi dans une structure `slideRef` qui associe chaque ObjectID du template à son équivalent dans la copie.

### Étape 3.3 — Suppression des slides originaux

Une fois toutes les slides du plan dupliquées, le programme supprime **tous les slides originaux** du template (ceux présents avant duplication). Ne restent que les copies correspondant au plan.

```
Copie du template (325 slides)
        |
        | DuplicateObject x N (une par slide du plan)
        v
325 slides originales + N copies
        |
        | DeleteObject x 325 (suppression de tous les originaux)
        v
N slides uniquement (celles du plan)
```

### Étape 3.4 — Réordonnancement

L'API `DuplicateObject` place les copies à côté de leur source, pas dans l'ordre du plan. Le programme utilise `UpdateSlidesPosition` pour remettre les slides dans le bon ordre. L'astuce : il itère en sens inverse, déplaçant chaque slide en position 0, ce qui produit l'ordre final correct.

### Étape 3.5 — Modification batch des contenus textuels

Pour chaque champ éditable marqué comme modifié dans le plan, le programme génère une série de requêtes API :

1. **`DeleteText`** — vide le texte existant de l'élément
2. **`InsertText`** — insère le nouveau texte
3. **`UpdateTextStyle`** — applique le gras et l'italique (si markdown détecté)
4. **`CreateParagraphBullets`** — convertit les lignes en listes à puces (si tirets détectés)

L'ordre d'exécution est critique (delete → insert → style → bullets) et géré par la fonction `SortRequests` du package `markdown/`.

Le **support markdown** est un "petit hack" : le package `markdown/markdown.go` utilise la bibliothèque `goldmark` pour parser le markdown en AST, puis traduit chaque noeud en une ou plusieurs requêtes de l'API Google Slides. Seul un sous-ensemble est supporté : **gras**, *italique*, et listes à puces (un ou deux niveaux d'indentation).

Toutes ces requêtes sont envoyées en un **seul appel `BatchUpdate`**, qui applique d'un coup l'ensemble des modifications textuelles à la présentation.

Le résultat : une URL Google Slides pointant vers la présentation finale, prête à être utilisée.

---

## Phase 4 : Post-production — correction automatique du formatage

Cette phase est **optionnelle** et peut être exécutée sur **n'importe quelle présentation** Google Slides, qu'elle ait été générée par ce système ou non. Elle détecte et corrige automatiquement les problèmes de formatage (polices, tailles, espacements) en comparant le rendu visuel aux données structurelles.

Le programme `fixfonts/main.go` orchestre les quatre étapes suivantes.

### Étape 4.1 — Export PDF via Google Drive API

Le programme exporte la présentation complète en PDF via `Drive.Files.Export(presentationID, "application/pdf")`. Ce PDF capture le rendu visuel tel que Google Slides l'affiche, y compris les débordements de texte et les incohérences visuelles qui ne sont pas détectables à partir des seules données structurelles.

### Étape 4.2 — Extraction de la structure via Google Slides API

En parallèle, le programme récupère la structure complète via `Presentations.Get(presentationID)` et en extrait un JSON structurel contenant, pour chaque élément texte de chaque slide :
- **Polices** (font family) et **tailles** (font size, en points)
- **Styles** (gras, italique)
- **Boîtes englobantes** (position et dimensions en EMU, converties en points : 1 pt = 12700 EMU)
- **Formatage de paragraphe** : espacement inter-lignes, espace avant/après
- **Cellules de tableau** : localisation par indices ligne/colonne

### Étape 4.3 — Analyse par Claude Opus (Vertex AI)

Le programme envoie à Claude Opus via l'API Vertex AI `rawPredict` :
1. Le **PDF** de la présentation (encodé en base64, type `document`)
2. Le **JSON structurel** extrait à l'étape précédente

```
PDF (rendu visuel) ---+
                      +----> Claude Opus ----> Plan de corrections (JSON)
JSON structurel    ---+      (Vertex AI)
```

Claude compare le rendu visuel aux données structurelles et détecte cinq catégories de problèmes :
- **Débordement de texte** : texte qui dépasse son conteneur
- **Tailles de police** trop grandes par rapport au conteneur
- **Polices inconsistantes** : familles de polices différentes là où l'uniformité est attendue
- **Espacement de lignes** : interligne trop serré ou trop lâche
- **Espacement de paragraphes** : espace avant/après inadéquat

Pour chaque problème détecté, Claude propose une correction précise : l'ObjectID de l'élément, le type de modification, et la valeur cible.

### Étape 4.4 — Validation et application des corrections

Le programme valide chaque correction proposée en vérifiant que l'ObjectID référencé existe bien dans la structure réelle de la présentation. Les corrections validées sont traduites en requêtes API :

- **`UpdateTextStyleRequest`** — modification de la taille de police et/ou de la famille de police (sur une plage de texte ou un élément entier)
- **`UpdateParagraphStyleRequest`** — modification de l'espacement inter-lignes et de l'espace avant/après

Toutes les corrections sont appliquées en un **seul appel `BatchUpdate`**, de la même manière que la Phase 3.

---

## Récapitulatif du flux de données

| Étape | Entrée | Traitement | Sortie |
|-------|--------|------------|--------|
| 1.1 | ID du template | `analysis/main.go` — API Google Slides | `content.json` (x N slides) |
| 1.2 | API Google Slides | Export d'images | `slide.png` (x N slides) |
| 1.3 | `slide.png` + `content.json` | Claude Opus 4.5 (Vision, Vertex AI) | `analysis.json` par slide |
| 1.4 | Tous les `analysis.json` | `buildTemplateIndex/` | `template_index.json` |
| 2.1 | `template_index.json` | Construction du prompt compact | Index compact (texte) |
| 2.2 | Index compact + demande | Claude Sonnet 4.5 (Vertex AI) | Plan brut (JSON) |
| 2.3 | Plan brut + `analysis.json` | Enrichissement | `PresentationPlan` (JSON) |
| 3.1 | ID du template | `Drive.Files.Copy` | Nouvelle présentation (copie) |
| 3.2 | `PresentationPlan` | `DuplicateObject` (x N) | Slides dupliquées avec IDs mappés |
| 3.3 | Slides originaux | `DeleteObject` (x 325) | Seules les copies restent |
| 3.4 | Slides dupliquées | `UpdateSlidesPosition` | Ordre final correct |
| 3.5 | Textes modifiés (markdown) | `BatchUpdate` (delete/insert/style/bullets) | Présentation finale |
| 4.1 | ID de présentation | `Drive.Files.Export` (PDF) | PDF de la présentation |
| 4.2 | Slides API | `fixfonts/main.go` — extraction structure | JSON structurel (polices, tailles, positions) |
| 4.3 | PDF + JSON structurel | Claude Opus (Vertex AI) | Plan de corrections (JSON) |
| 4.4 | Plan de corrections | `BatchUpdate` (UpdateTextStyle/UpdateParagraphStyle) | Présentation corrigée |
