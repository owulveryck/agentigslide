# Rationale : pourquoi un orchestrateur Go plutôt que Claude Code ou l'Agent SDK

Ce document explique le choix architectural d'implémenter un orchestrateur multi-agents en Go natif, plutôt que d'utiliser Claude Code (équipé de serveurs MCP) ou l'Anthropic Agent SDK comme backend d'orchestration.

## Contexte

Le système agentigslide est un pipeline multi-agents complet :

- **Outliner** → **Selector** → **Writers/Designers** (parallèles) → **Assembler** → **Reviewer** (avec boucle de feedback)
- État partagé typé (`PipelineState`) protégé par mutex
- Validation programmatique entre chaque étape
- Prompt caching Vertex AI partagé entre Writers parallèles
- Boucles de retry avec feedback structuré (`ReviewIssue[]`)
- Collecte de métriques (tokens, durée, cache hit)

## Claude Code + MCP : faisable, mais limité

Il aurait été techniquement possible d'équiper Claude Code avec des serveurs MCP pour Google Slides API (BatchUpdate, DuplicateObject…) et Google Drive API (CopyFile), en utilisant Claude Code comme cerveau orchestrateur.

Les limitations auraient cependant été significatives :

### 1. Pas de parallélisme contrôlé

Le système lance des Writers en parallèle avec un sémaphore (`MaxParallel=5`) et des goroutines Go. Claude Code exécute les tool calls de manière séquentielle ou en parallèle simple — sans contrôle fin sur la concurrence ni sur l'accès concurrent à l'état partagé (mutex sur `PipelineState`).

### 2. Boucles de feedback structurées

Le Reviewer renvoie des `ReviewIssue[]` typés vers les Writers spécifiques, avec :
- Compteur de retries
- Filtrage des issues non-corrigeables (`wrong_template`)
- Re-review ciblé sur le sous-ensemble de slides corrigées

Reproduire cette logique dans un prompt Claude Code serait fragile — le modèle pourrait perdre des contraintes au fil des itérations conversationnelles.

### 3. Structured output et validation

Chaque agent est forcé d'utiliser `tool_use` avec un JSON schema strict, puis la sortie est validée programmatiquement (`ValidateOutline`, `ValidateSelection`, `ValidateSelectionGlobal`). Claude Code ne fournit pas ce niveau de contrôle sur le format des sorties intermédiaires entre "agents".

### 4. Prompt caching et coût

Le système exploite le cache éphémère Vertex AI sur les blocs système (catalogue de templates, mémoire agent) partagés entre les Writers parallèles. Avec Claude Code, chaque appel d'agent serait un nouveau contexte conversationnel — pas de cache partagé, donc un coût en tokens significativement plus élevé.

### 5. État mutable partagé

`PipelineState` est un état Go typé avec mutex, contenant l'outline, les sélections, les contenus de slides, les spécifications de diagrammes et le plan assemblé. Dans Claude Code, cet état vivrait dans le contexte conversationnel — fragile, non typé, sujet à la dérive contextuelle.

### 6. Observabilité et métriques

Le système collecte les tokens (input, output, cache read, cache creation) par agent, la durée du pipeline, et un issue log complet. Cette granularité d'observabilité serait très difficile à reproduire dans un flow Claude Code.

## Agent SDK (Anthropic)

L'Agent SDK (Python/TypeScript) aurait été un meilleur candidat que Claude Code brut, car il fournit :
- Orchestration d'agents avec tool use
- Structured output via JSON schema
- Boucles de conversation multi-tours

Mais plusieurs limitations subsistent :

- **Parallélisme** : l'Agent SDK est plus séquentiel que les goroutines Go avec sémaphore
- **Vertex AI** : l'Agent SDK utilise l'API Anthropic directe, pas les endpoints Vertex AI — incompatible avec l'authentification GCP et le prompt caching Vertex
- **Validation programmatique** : la validation typée en Go entre chaque étape (schemas, contraintes métier) est plus naturelle dans un langage typé statiquement
- **Prompt caching** : le caching éphémère tel qu'utilisé (blocs système partagés entre Writers) n'est pas directement transposable

## Comparaison

| Critère | Go natif | Claude Code + MCP | Agent SDK |
|---------|----------|-------------------|-----------|
| Parallélisme fin | Goroutines + sémaphore | Limité | Moyen |
| Feedback loops typés | `ReviewIssue[]` + retry ciblé | Conversationnel, fragile | Bon |
| Structured output | JSON schema + `tool_use` forcé | Non contrôlé | Bon |
| Prompt caching | Vertex AI éphémère, partagé | Non disponible | Partiel (API directe) |
| Validation inter-étapes | Typée, programmatique | Implicite | Programmatique |
| Coût de développement initial | Élevé | Faible | Moyen |
| Maintenabilité | Typé, testable | Fragile (conversationnel) | Moyen |
| Observabilité | Métriques par agent | Limitée | Partielle |
| Intégration Vertex AI | Native | Non | Non |

## Conclusion

Le choix d'un orchestrateur Go natif est justifié par le niveau de complexité du pipeline : feedback loops typés, parallélisme contrôlé, validation stricte entre étapes, et intégration Vertex AI native avec prompt caching.

Claude Code + MCP aurait pu servir pour un **prototype rapide** ou un pipeline linéaire simple, mais pas pour un système de production avec ces exigences. L'Agent SDK serait un bon compromis pour un projet plus simple ou n'utilisant pas Vertex AI.
