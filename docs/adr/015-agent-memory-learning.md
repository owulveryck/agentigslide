# ADR 015 : Memoire d'apprentissage par agent

- **Date** : 2026-05-29
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline multi-agent (ADR 001) et le pipeline d'edition (ADR 011) executent une boucle de feedback intra-run : le Reviewer detecte des problemes (overflow, text_density, duplicate, etc.), les Writers corrigent dans la meme execution, et le Reviewer re-valide. Le Selector fait des retries sur erreurs de validation. La review visuelle (ADR 012) detecte et corrige des problemes visuels.

Cependant, toutes ces lecons sont perdues a la fin de chaque execution. Chaque nouveau run demarre sans connaissance des erreurs passees. Cela entraine :

1. **Repetition des memes erreurs** : les agents font les memes erreurs sur les memes slides template apres template (ex: overflow systematique sur un champ a capacite reduite).

2. **Cout inutile** : chaque iteration de correction consomme des tokens LLM et du temps. Prevenir les erreurs serait moins couteux que les corriger.

3. **Pas de capitalisation** : les patterns d'echec specifiques a un template (ex: "le slide #42 deborde toujours au-dela de 120 caracteres") ne sont jamais captures.

## Decision

### Fichiers memoire par agent par template

Chaque agent dispose d'un fichier memoire par template, stocke dans le repertoire du template :

```
template/{TEMPLATE_ID}/OUTLINER_MEMORY.md
template/{TEMPLATE_ID}/SELECTOR_MEMORY.md
template/{TEMPLATE_ID}/WRITER_MEMORY.md
template/{TEMPLATE_ID}/REVIEWER_MEMORY.md
template/{TEMPLATE_ID}/DESIGNER_MEMORY.md
template/{TEMPLATE_ID}/EDITPLANNER_MEMORY.md
template/{TEMPLATE_ID}/EDITWRITER_MEMORY.md
template/{TEMPLATE_ID}/EDITREVIEWER_MEMORY.md
```

Le format est du Markdown avec des guidelines actionnables, lisibles par un humain. Le choix du Markdown (plutot que JSON ou une base de donnees) permet :
- Versionnement avec git aux cotes du template
- Edition manuelle par l'utilisateur
- Injection directe dans le prompt sans transformation

### Injection dans les prompts

`BuildSystemBlocks()` dans `internal/agent/cache.go` est etendu avec un parametre `agentMemory` :

```go
func BuildSystemBlocks(systemPrompt, templateInstructions, agentMemory string) []vertex.ContentBlock
```

L'ordre des blocs est : systemPrompt -> agentMemory -> templateInstructions. Le cache breakpoint reste sur le dernier bloc pour maximiser le prompt caching (le contenu memoire change rarement entre calls d'un meme run).

Le contenu memoire est prefixe par :
```
MEMOIRE DE L'AGENT (guidelines issues des executions precedentes) :
```

Chaque agent charge sa memoire via `LoadAgentMemory(templateDir, agentName)`, calquee sur `LoadTemplateInstructions()`.

### Collecte des erreurs pendant le pipeline

Un nouveau type `IssueRecord` accumule toutes les issues detectees a travers toutes les iterations :

```go
type IssueRecord struct {
    Agent     string
    Iteration int
    Issues    []ReviewIssue
    Resolved  bool
}
```

Les points de collecte dans les orchestrateurs :
- Apres chaque `runReviewer()` / `runReviewerSubset()` : enregistrer les issues
- Apres chaque retry Selector : enregistrer les erreurs de validation
- Comparaison entre iterations pour marquer les issues resolues

### Synthese en fin de pipeline avec confirmation utilisateur

Apres la completion du pipeline (fixfonts inclus), une etape optionnelle de synthese :

1. Si aucune erreur detectee durant le run : pas de synthese
2. Charger les memoires existantes pour chaque agent
3. Appeler un LLM rapide (haiku) pour analyser les erreurs et produire des guidelines mises a jour
4. Afficher les propositions sur stderr
5. Demander confirmation a l'utilisateur avant ecriture

Le choix de la confirmation utilisateur plutot que l'ecriture automatique evite l'accumulation de guidelines erronees ou bruyantes. L'utilisateur peut aussi editer manuellement les fichiers apres coup.

Le modele de synthese est configurable via `AGENT_MEMORY_MODEL` (default: haiku) pour minimiser le cout.

## Alternatives evaluees

### Base de donnees SQLite pour l'historique des erreurs

Stocker chaque issue dans une base SQLite avec horodatage, template, slide, type d'erreur. Rejete : surdimensionne pour le besoin actuel, non versionnable avec git, et les guidelines synthetisees en Markdown sont plus directement utilisables dans les prompts.

### Ecriture automatique sans confirmation

Les guidelines sont ecrites directement apres synthese. Rejete par choix utilisateur : risque d'accumulation de guidelines erronees ou contradictoires. La confirmation humaine garantit la qualite.

### Memoire globale partagee entre agents

Un seul fichier `MEMORY.md` pour tous les agents. Rejete : chaque agent a des preoccupations differentes (le Writer se soucie du depassement de caracteres, le Selector de la correspondance template/contenu). Des fichiers separes evitent de polluer le contexte d'un agent avec des guidelines irrelevantes.

### Memoire uniquement pour le Writer

Commencer par un seul agent puisque le Writer genere la majorite des erreurs. Rejete : le Selector fait aussi des erreurs systematiques (mauvaise correspondance template) et le Reviewer pourrait beneficier de guidelines sur les faux positifs recurrents.

## Consequences

### Positives

- **Amelioration incrementale** : chaque execution enrichit la connaissance des agents pour le template
- **Reduction des retries** : les erreurs evitees en premiere passe eliminent des iterations couteuses
- **Transparence** : les guidelines sont en Markdown lisible, editables par l'utilisateur
- **Cout marginal** : un seul appel haiku en fin de pipeline pour la synthese
- **Versionnable** : les fichiers memoire sont commites avec le template

### Negatives

- **Latence en fin de pipeline** : un appel LLM supplementaire pour la synthese
- **Risque de guidelines obsoletes** : si le template evolue, les guidelines peuvent devenir incorrectes (attenuee par la review humaine)
- **Augmentation du prompt** : chaque agent recoit un bloc supplementaire, ce qui augmente legerement les tokens d'entree (attenuee par le prompt caching)

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENT_MEMORY_ENABLED` | `true` | Active le chargement et la synthese de memoire |
| `AGENT_MEMORY_MODEL` | `claude-haiku-4-5@20251001` | Modele pour la synthese des guidelines |

## Fichiers concernes

| Fichier | Modification |
|---------|-------------|
| `internal/agent/types.go` | Ajout : `IssueRecord`, `IssueLog`, champs `AgentMemories` et `IssueLog` dans `PipelineState` |
| `internal/agent/cache.go` | Modifie : parametre `agentMemory` dans `BuildSystemBlocks()` |
| `internal/agent/config.go` | Ajout : `MemoryEnabled`, `MemoryModel` |
| `internal/agent/memory.go` | Nouveau : `SynthesizeMemory()`, `ProposeMemoryUpdates()`, `WriteMemoryFiles()` |
| `internal/pipeline/pipeline.go` | Ajout : `LoadAgentMemory()` |
| `internal/agent/orchestrator/orchestrator.go` | Modifie : collecte des issues, chargement memoire |
| `internal/agent/editorchestrator/editorchestrator.go` | Modifie : idem pour le pipeline d'edition |
| `internal/agent/*/` (8 agents) | Modifie : parametre `agentMemory` dans les methodes Run/Write |
| `cmd/slidegen/main.go` | Modifie : appel synthese apres pipeline |
| `cmd/slidegen/edit.go` | Modifie : appel synthese apres post-traitement |
