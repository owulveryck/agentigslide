# Serveur MCP slidegen

Serveur [Model Context Protocol](https://modelcontextprotocol.io/) qui expose le pipeline de generation de presentations comme outil MCP `generate_slides`. Un chatbot ou un agent IA peut appeler cet outil avec du contenu markdown et recevoir l'URL de la presentation Google Slides creee.

## Prerequis

### Variables d'environnement

Le serveur utilise la meme configuration que le CLI `slidegen` :

```bash
# Template Google Slides (obligatoire)
export SLIDES_TEMPLATE_ID="YOUR_TEMPLATE_PRESENTATION_ID"
export SLIDES_CREDENTIALS="/path/to/oauth2-credentials.json"

# Vertex AI (obligatoire)
export VERTEX_PROJECT_ID="your-gcp-project-id"
export VERTEX_REGION="us-east5"  # defaut

# Modeles agents (optionnel, valeurs par defaut)
export AGENT_OUTLINER_MODEL="claude-sonnet-4-6"
export AGENT_SELECTOR_MODEL="claude-sonnet-4-6"
export AGENT_WRITER_MODEL="claude-sonnet-4-6"
export AGENT_REVIEWER_MODEL="claude-opus-4-6"
```

Utiliser `-h` pour afficher toutes les variables disponibles avec leurs valeurs par defaut.

### Credentials Google

Le serveur necessite des credentials OAuth2 pour acceder aux API Google Slides et Drive. Le fichier JSON est specifie via `SLIDES_CREDENTIALS`.

## Utilisation

### Lancement

```bash
# Mode stdio (defaut) -- integration directe avec Claude Code ou agents MCP
go run exp/mcp-server/main.go

# Mode SSE -- Server-Sent Events pour clients web
go run exp/mcp-server/main.go --mode sse --addr :8080

# Mode HTTP streamable -- bidirectionnel avec protection cross-origin
go run exp/mcp-server/main.go --mode http --addr :8080 --allow-origin https://example.com
```

### Integration Claude Code

Ajouter dans `.mcp.json` a la racine du projet :

```json
{
  "mcpServers": {
    "slidegen": {
      "command": "go",
      "args": ["run", "exp/mcp-server/main.go"],
      "env": {
        "SLIDES_TEMPLATE_ID": "${SLIDES_TEMPLATE_ID}",
        "SLIDES_CREDENTIALS": "${SLIDES_CREDENTIALS}",
        "VERTEX_PROJECT_ID": "${VERTEX_PROJECT_ID}"
      }
    }
  }
}
```

Les variables `${...}` sont resolues depuis l'environnement au lancement.

## Outil expose : `generate_slides`

### Entree

Contenu markdown decrivant la presentation :

```markdown
Innovation et Transformation Digitale 2026

# Introduction
Notre strategie d'innovation repose sur trois piliers fondamentaux.

## Cloud Native
- Migration vers Kubernetes
- Architecture microservices

# Conclusion
La transformation digitale est un levier strategique.
```

### Sortie

URL de la presentation Google Slides creee :

```
https://docs.google.com/presentation/d/{id}/edit
```

## Modes de transport

| Mode | Flag | Usage |
|------|------|-------|
| **stdio** | `--mode stdio` (defaut) | Processus local, communication via stdin/stdout. Ideal pour Claude Code et les agents MCP locaux |
| **SSE** | `--mode sse` | Server-Sent Events. Necessite `--addr` pour le port d'ecoute. Support CORS via `--allow-origin` |
| **HTTP** | `--mode http` | HTTP streamable bidirectionnel. Protection cross-origin integree via `--allow-origin` |

## Erreurs structurees

Les erreurs retournees suivent une categorisation en 3 types (voir [ADR 008](../../docs/adr/008-structured-mcp-errors.md)) :

| Categorie | Retryable | Exemple |
|-----------|-----------|---------|
| `validation` | non | Contenu vide |
| `transient` | oui | Timeout API Vertex AI, rate limit |
| `business` | non | Aucun template ne correspond au contenu |

Le format du message d'erreur :

```
[transient] Agent pipeline failed: context deadline exceeded
Retryable: true
```

## Architecture

```
MCP Client (Claude Code, agent)
    |
    | MCP protocol (stdio / SSE / HTTP)
    v
MCP Server (ce programme)
    |
    | Go function calls
    v
Pipeline multi-agent
  Outliner -> Selector -> Writers (paralleles) -> Reviewer
    |
    | Google Slides API + Drive API
    v
Presentation Google Slides
```

Le serveur reutilise exactement le meme pipeline que le CLI `slidegen` -- pas d'implementation separee.

## Limitations

- **Temps de traitement** : 60-180 secondes (pipeline multi-agent + appels API Google)
- **Contenu en francais** : les templates utilisent la typographie francaise
- **Credentials Google** : necessite un fichier OAuth2 avec acces aux API Slides et Drive
- **Statut experimental** : ce serveur est dans `exp/`, l'API peut changer
