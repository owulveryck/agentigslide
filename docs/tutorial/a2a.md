# Tutoriel : Agents A2A standalone

Ce guide explique comment lancer les agents agentigslide comme serveurs A2A indépendants et interagir avec eux via un [A2A Inspector](https://github.com/a2aproject/a2a-inspector).

## Prérequis

- Go 1.24+
- Projet Google Cloud avec l'API Vertex AI activée
- Credentials par défaut configurés : `gcloud auth application-default login`
- Template index construit : `go run buildTemplateIndex/build_template_index.go`
- (Orchestrateur uniquement) Credentials OAuth2 Google Slides/Drive

## Variables d'environnement

```bash
# Vertex AI (obligatoire)
export VERTEX_PROJECT_ID="your-gcp-project-id"
export VERTEX_REGION="us-east5"

# Agents (optionnel — les défauts sont raisonnables)
export AGENT_OUTLINER_MODEL="claude-sonnet-4-6"
export AGENT_SELECTOR_MODEL="claude-sonnet-4-6"
export AGENT_WRITER_MODEL="claude-sonnet-4-6"
export AGENT_REVIEWER_MODEL="claude-opus-4-6"

# Slides (obligatoire pour l'orchestrateur)
export SLIDES_TEMPLATE_ID="YOUR_TEMPLATE_PRESENTATION_ID"
export SLIDES_CREDENTIALS="/path/to/oauth2-credentials.json"
```

## Lancement individuel

Chaque agent se lance indépendamment depuis la racine du dépôt :

```bash
# Outliner — structure la demande en plan de présentation
go run cmd/outliner/main.go --addr :8080

# Selector — mappe les besoins de slides aux templates du catalogue
go run cmd/selector/main.go --addr :8081

# Writer — génère le contenu textuel pour chaque slide
go run cmd/writer/main.go --addr :8082

# Reviewer — valide la qualité du plan assemblé
go run cmd/reviewer/main.go --addr :8083

# Orchestrator — pipeline complet, crée la présentation Google Slides
go run cmd/orchestrator/main.go --addr :8084
```

## Lancement de tous les agents

Le script `exp/scripts/agents.sh` lance tous les agents en une commande :

```bash
./exp/scripts/agents.sh
```

Les agents outliner, selector, writer et reviewer démarrent en arrière-plan. L'orchestrateur démarre au premier plan — `Ctrl+C` arrête tout.

## Vérification

Vérifier qu'un agent est accessible via son Agent Card :

```bash
curl -s http://localhost:8080/.well-known/agent-card.json | jq .
```

Résultat attendu :

```json
{
  "name": "Outliner",
  "description": "Analyse une demande utilisateur et produit un plan...",
  "version": "0.1.0",
  "skills": [...]
}
```

Vérifier tous les agents :

```bash
for port in 8080 8081 8082 8083 8084; do
  echo "--- :$port ---"
  curl -s http://localhost:$port/.well-known/agent-card.json | jq .name
done
```

## Test avec A2A Inspector

### Outliner (port 8080)

Connecter l'inspector à `http://localhost:8080`. Envoyer un message texte :

```
Crée une présentation de 8 slides sur les bonnes pratiques DevOps pour une équipe de développement.
```

L'outliner retourne un artifact JSON contenant le `PresentationOutline` structuré.

### Selector (port 8081)

Envoyer un **data part** JSON :

```json
{
  "outline": { "...PresentationOutline du outliner..." },
  "compactCatalog": "SLIDE 1 | Titre section | ...\nSLIDE 2 | ..."
}
```

### Writer (port 8082)

Envoyer un **data part** JSON :

```json
{
  "sourceSlide": 5,
  "slideNeed": {
    "intent": "Présenter les 3 piliers du DevOps",
    "contentItems": ["Culture", "Automatisation", "Mesure"],
    "type": "content"
  },
  "templateFields": [
    {"variableName": "titleMainShape", "role": "titre principal", "maxChars": 50},
    {"variableName": "bodyTextShape", "role": "contenu principal", "maxChars": 300}
  ]
}
```

### Reviewer (port 8083)

Envoyer un **data part** JSON :

```json
{
  "plan": { "...GenerationPlan assemblé..." },
  "userRequest": "Présentation DevOps en 8 slides",
  "compactCatalog": "SLIDE 1 | ..."
}
```

### Orchestrator (port 8084)

Connecter l'inspector à `http://localhost:8084`. Envoyer un message texte :

```
Crée une présentation de 5 slides pour le comité de direction sur les résultats Q1 2026.
Inclure : chiffre d'affaires, croissance clients, roadmap technique, prochaines étapes.
```

L'orchestrateur exécute le pipeline complet et retourne l'URL de la présentation créée.

## Architecture

```
A2A Inspector
     │
     ▼
┌─────────────┐
│ Orchestrator │ :8084  ← texte → URL Google Slides
│  (pipeline)  │
└──────┬──────┘
       │ appels in-process
       ├──→ Outliner   :8080  (disponible aussi en standalone)
       ├──→ Selector   :8081  (disponible aussi en standalone)
       ├──→ Writer(s)  :8082  (disponible aussi en standalone)
       └──→ Reviewer   :8083  (disponible aussi en standalone)
```

> **Note :** L'orchestrateur appelle actuellement les agents en mode in-process (appels Go directs). Les serveurs A2A standalone permettent de tester et déboguer chaque agent individuellement. La migration vers des appels A2A inter-agents est prévue dans une phase ultérieure (cf. [ADR 007](../adr/007-a2a-architecture.md)).

## Dépannage

| Symptôme | Cause probable | Solution |
|----------|---------------|----------|
| `Vertex configuration error` | `VERTEX_PROJECT_ID` non défini | Exporter la variable |
| `Failed to load template index` | Index non construit | Exécuter `go run buildTemplateIndex/build_template_index.go` |
| `Failed to get authenticated client` | Credentials OAuth manquants | Définir `SLIDES_CREDENTIALS` ou `gcloud auth application-default login` |
| Port déjà utilisé | Un agent tourne déjà | Utiliser `--addr :PORT` avec un port différent |
