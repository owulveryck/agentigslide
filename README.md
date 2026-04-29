# slideAppScripter

Generateur automatique de presentations Google Slides a partir d'un template OCTO et d'une demande en texte libre ou markdown.

Le systeme analyse un template de slides preformatees avec Claude Vision, construit un index cherchable, puis utilise Claude pour selectionner les slides et remplir le contenu a partir d'une demande utilisateur.

## Prerequis

- Go 1.24+
- Un projet Google Cloud avec l'API Vertex AI activee
- Un template Google Slides (presentation preformatee)
- Des credentials Google Cloud (default credentials ou OAuth2 client)

## Variables d'environnement

```bash
export SLIDES_PREFORMATES_ID="<id-de-la-presentation-template>"
export ANTHROPIC_VERTEX_PROJECT_ID="<id-projet-gcp>"
export CLOUD_ML_REGION="us-east5"
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/credentials.json"
```

## Utilisation

Le workflow se decompose en trois phases. Les phases 1 et 2 sont executees une seule fois pour un template donne. La phase 3 est executee a chaque demande de generation.

### Phase 1 : Analyser le template

#### 1.1 Extraire les donnees brutes depuis Google Slides

```bash
go run analysis/main.go
```

Se connecte a l'API Google Slides et sauvegarde pour chaque slide :
- `template/{presentationID}/{slideNumber}/content.json` : structure complete de la slide (ObjectIDs, positions, texte, placeholders)
- `template/{presentationID}/{slideNumber}/slide.png` : image de la slide

#### 1.2 Analyser les slides avec Claude Vision

```bash
go run analyzeSlides/analyze_slides.go --slides 1,2,5,10,20,30,40,50
```

Envoie chaque slide (image + resume textuel) a Claude Opus via Vertex AI. Produit pour chaque slide :
- `analysis.json` : elements editables (titres, textes, annee...) et elements visuels (icones, images), chacun avec son ObjectID
- `analysis.md` : description lisible de la slide

Le flag `--slides` permet de ne traiter qu'un sous-ensemble de slides (par numero, separes par des virgules).

### Phase 2 : Construire l'index

```bash
go run buildTemplateIndex/build_template_index.go
```

Agrege tous les `analysis.json` en un fichier `template_index.json`. Pour chaque slide :
- Extrait des mots-cles
- Infere un role semantique pour chaque element editable (`titre_principal`, `sous_titre`, `corps_texte`, `annee`...)
- Genere des noms de variables uniques (`titlemainShape`, `textmiddlecenterShape`, `textmiddlecenter2Shape`...)
- Extrait le contenu reel de chaque element depuis `content.json`
- Resout les coordonnees des cellules de tableau

Le resultat est le catalogue complet du template, utilisable par l'IA pour selectionner et remplir les slides.

### Phase 3 : Generer une presentation

```bash
go run slidegen/main.go --file request.md --credentials ~/.config/gcloud/slideappscripter-client.json
```

Genere une presentation complete en un seul appel :

1. Charge `template_index.json` et construit un index compact
2. Envoie la demande utilisateur + l'index a Claude via Vertex AI
3. Claude selectionne les slides, leur ordre, et le texte a inserer dans chaque champ
4. Copie le template via Google Drive API
5. Duplique les slides choisies avec un mapping d'IDs predictible
6. Supprime les slides originales du template
7. Applique les modifications textuelles (avec support markdown : **gras**, *italique*, listes a puces)
8. Retourne l'URL de la presentation finale

Le fichier `request.md` contient la demande en texte libre ou markdown. Exemple :

```markdown
# Ma presentation

## Introduction

Le projet vise a automatiser la creation de slides...

## Les 3 etapes

- Etape 1 : Analyse du template
- Etape 2 : Planification par IA
- Etape 3 : Production via API Google
```

#### Options alternatives

```bash
# Generer uniquement le plan JSON (sans creer la presentation)
go run generateSlideList/generate_slide_list.go --request "Description de la presentation"

# Mode interactif (multi-lignes)
go run generateSlideList/generate_slide_list.go --interactive

# Appliquer un plan JSON existant
go run applySlideList/apply_slide_list.go --plan plan.json

# Pipeline : generation + application
go run generateSlideList/generate_slide_list.go --request "..." | go run applySlideList/apply_slide_list.go --plan -
```

## Structure du projet

```
analysis/              Extraction des donnees brutes depuis Google Slides API
analyzeSlides/         Analyse par Claude Vision (Vertex AI)
buildTemplateIndex/    Construction de template_index.json
generateSlideList/     Generation du plan de slides (JSON)
applySlideList/        Application du plan pour creer la presentation
slidegen/              Flux tout-en-un (plan + creation)
markdown/              Parsing markdown et generation de requetes Google Slides
template/              Donnees extraites du template (content.json, slide.png, analysis.json)
docs/                  Documentation d'architecture
```

## Format des donnees

### template_index.json

```json
{
  "templateId": "...",
  "slides": [
    {
      "slideNumber": 1,
      "slideId": "g344a...",
      "intention": "Slide de couverture",
      "keywords": ["couverture", "titre", "octo"],
      "editableFields": [
        {
          "objectId": "g3b45...",
          "role": "titre_principal",
          "variableName": "titlemainShape",
          "content": "Slides preformatees",
          "rawContent": "Slides preformatees"
        }
      ]
    }
  ]
}
```

### Support markdown dans les textes

Les champs `newText` du plan supporte un sous-ensemble de markdown :
- `**gras**` pour la mise en valeur
- `*italique*` pour les nuances
- Listes a puces avec `-` (un ou deux niveaux d'indentation)
