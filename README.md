# AgentiGSlide

Generateur automatique de presentations Google Slides a partir d'un template et d'une demande en texte libre ou markdown. Le systeme utilise Claude (via Vertex AI) pour analyser un template, puis selectionner et remplir les slides adaptees a chaque demande.

## Prerequis

- Go 1.25+
- Un projet Google Cloud avec l'API Vertex AI activee
- Un template Google Slides
- `gcloud` CLI installe et configure

## Quickstart

### 1. Authentification

```bash
gcloud auth login
gcloud auth application-default login
```

### 2. Configuration

```bash
export SLIDES_TEMPLATE_ID="<id-de-la-presentation-template>"
export VERTEX_PROJECT_ID="<id-projet-gcp>"
```

### 3. Analyser le template (une seule fois)

```bash
make template
```

Execute les trois phases d'analyse : extraction des donnees depuis Google Slides, analyse par Claude Vision, construction de l'index cherchable.

### 4. Compiler les outils

```bash
make
```

### 5. Generer une presentation

```bash
bin/slidegen --file request.md
```

Le fichier `request.md` contient la demande en texte libre ou markdown.

> Chaque outil supporte `-h` pour afficher toutes les variables d'environnement et les flags disponibles.

## Pipeline

Le workflow se decompose en trois phases principales, suivies d'une phase optionnelle :

1. **Analyse** -- extraction et analyse par IA vision du template (`make template`)
2. **Planification** -- selection des slides et du contenu par Claude
3. **Production** -- duplication du template et application des modifications via les API Google
4. **Post-production** *(optionnelle)* -- correction automatique du formatage (polices, tailles, espacements)

`slidegen` execute les phases 2 et 3 en un seul appel. Le mode `--agent` utilise un pipeline multi-agent (Outliner/Selector/Writers/Reviewer).

Voir [docs/architecture.md](docs/architecture.md) pour le detail de chaque phase.

## Utilisation de slidegen

```bash
# Generation standard
bin/slidegen --file request.md

# Pipeline multi-agent
bin/slidegen --agent --file request.md

# Dashboard de suivi en temps reel
bin/slidegen --agent --monitor --file request.md

# Reprendre a partir d'un plan sauvegarde
bin/slidegen --plan plan.json

# Afficher le prompt sans executer
bin/slidegen --dump --file request.md
```

## Structure du projet

```
analysis/              Extraction des donnees brutes depuis Google Slides API
analyzeSlides/         Analyse par Claude Vision (Vertex AI)
buildTemplateIndex/    Construction de template_index.json
slidegen/              Flux tout-en-un (planification + production)
generateSlideList/     Generation du plan de slides (JSON)
applySlideList/        Application d'un plan pour creer la presentation
fixfonts/              Post-production : correction automatique du formatage
mcp-server/            Serveur MCP (Model Context Protocol)
markdown/              Parsing markdown et generation de requetes Google Slides
internal/              Packages partages (config, vertex, auth, pipeline, agent...)
template/              Donnees extraites du template
docs/                  Documentation d'architecture et diagrammes
```

## Licence

MIT -- voir [LICENSE](LICENSE).
