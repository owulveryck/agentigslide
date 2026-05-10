# ADR 008 : Erreurs structurees dans le serveur MCP

- **Date** : 2026-05-10
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le serveur MCP (`exp/mcp-server/`) expose le pipeline de generation de presentations comme outil MCP `generate_slides`. Les erreurs sont retournees via `SetError()` du SDK MCP Go (v1.6.0) qui positionne le flag `IsError: true` et place le message dans le `Content`.

Probleme : les messages d'erreur sont des strings non structures. Un agent appelant ne peut pas distinguer :
- une erreur transitoire (timeout API, rate limit) -> retry recommande
- une erreur de validation (contenu vide) -> corriger les parametres
- une erreur metier (contenu incompatible avec les templates) -> informer l'utilisateur

L'ADR 007 (architecture A2A) prevoit que les agents deviennent des services independants. La strategie d'erreurs structurees definie ici s'appliquera aux futures interfaces inter-agents.

## Decision

### Categoriser chaque erreur MCP avec trois dimensions

1. **errorCategory** : `validation` | `transient` | `business`
2. **isRetryable** : boolean indiquant si un retry a des chances de reussir
3. **description** : message lisible par un humain ou un agent

### Format de transmission

Le SDK MCP Go v1.6.0 ne supporte que `SetError(err error)` avec un flag `IsError bool`. Il n'y a pas de champ structure pour la categorie ou le retry. La categorisation est donc encodee dans le texte du contenu :

```
[validation] Empty content: provide markdown text describing the presentation to generate
Retryable: false
```

Ce format est parsable par un agent appelant (prefixe entre crochets + ligne `Retryable:`) tout en restant lisible par un humain.

### Categorisation des erreurs existantes

| Erreur | Categorie | Retryable | Justification |
|--------|-----------|-----------|---------------|
| Contenu vide | `validation` | false | Input invalide, retry identique = meme resultat |
| Pipeline failed (timeout/rate limit) | `transient` | true | Erreur temporaire API, retry peut reussir |
| Pipeline failed (autre) | `business` | false | Probleme structurel dans le contenu ou la configuration |
| Plan sans slides | `business` | false | Le contenu ne correspond pas aux templates disponibles |
| Creation presentation echouee | `transient` | true | Erreur Google Slides API, potentiellement temporaire |

### Detection des erreurs transitoires du pipeline

Le pipeline peut echouer pour des raisons transitoires (timeout Vertex AI, rate limit 429/529) ou structurelles. La fonction `isTransientPipelineError()` inspecte le message d'erreur pour des indicateurs connus : codes HTTP 429/529, timeouts, deadlines. En absence d'indicateur, l'erreur est classee `business`.

## Choix Techniques

### Texte structure vs champ JSON additionnel

Le SDK MCP actuel ne propose pas de champ `metadata` ou `errorCategory` sur `CallToolResult`. Les alternatives evaluees :
- **JSON dans le content** : plus parsable mais moins lisible pour les humains
- **Texte avec prefixe** : compromis lisibilite/parseabilite retenu

Quand le SDK evoluera pour supporter des metadonnees structurees sur les erreurs, la migration sera directe.

### Coherence avec le retry interne Vertex AI

Le client Vertex AI (`internal/vertex/client.go`) utilise deja `isRetryable(statusCode)` pour les retries internes (429, 529, 5xx). La detection des erreurs transitoires du MCP reutilise la meme logique au niveau superieur.

## Consequences

### Positives

- **Recovery intelligente** : un agent appelant peut implementer des strategies differenciees (retry, correction, escalade)
- **Observabilite** : les categories d'erreurs sont loguees et monitorables
- **Coherence A2A** : le pattern est reutilisable pour les futures interfaces inter-agents (ADR 007)
- **Retrocompatibilite** : `IsError: true` est toujours positionne, les clients existants continuent de fonctionner

### Negatives

- **Parsing textuel** : la categorisation est encodee dans le texte, pas dans un champ structure JSON. Fragile si le format change
- **Heuristique de detection** : `isTransientPipelineError()` repose sur l'inspection de strings d'erreur, ce qui est sujet a des faux negatifs si les messages changent

## Fichiers concernes

| Fichier | Modification |
|---------|-------------|
| `exp/mcp-server/main.go` | `structuredError()`, `isTransientPipelineError()`, categorisation des 4 erreurs |
| `exp/mcp-server/main_test.go` | Tests des erreurs structurees |
| `docs/architecture.md` | Section serveur MCP |
