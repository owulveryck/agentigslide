# Boucle de feedback du Reviewer

Le Reviewer est le dernier agent du pipeline. Il valide le plan assemblé et, s'il détecte des problèmes, déclenche une boucle de correction ciblée. La logique est dans [`internal/agent/orchestrator/orchestrator.go`](../internal/agent/orchestrator/orchestrator.go).

## Types

Définis dans [`internal/agent/types.go`](../internal/agent/types.go) :

```go
type ReviewIssue struct {
    SlideIndex  int    `json:"slideIndex"`
    Field       string `json:"field,omitempty"`
    IssueType   string `json:"issueType"`
    Description string `json:"description"`
    Suggestion  string `json:"suggestion"`
}

type ReviewResult struct {
    Approved bool          `json:"approved"`
    Issues   []ReviewIssue `json:"issues"`
}
```

### Types d'issues

| IssueType | Description |
|-----------|-------------|
| `overflow` | Contenu trop long pour le champ |
| `text_density` | Trop de texte sur une slide |
| `inappropriate_bullets` | Listes à puces mal utilisées |
| `duplicate` | Contenu dupliqué entre slides |
| `missing_content` | Contenu manquant par rapport à la demande |
| `wrong_template` | Template inadapté (non corrigeable par le Writer) |
| `incoherence` | Incohérence entre slides |
| `invented_content` | Contenu inventé, absent de la demande |
| `diagram_topology` | Problème de topologie d'un diagramme |

## Mécanisme de la boucle

```
Reviewer
  │
  ├─ approved=true → pipeline terminé
  │
  └─ approved=false, issues=[...]
       │
       ├─ handleReviewIssuesReturn()
       │    ├─ Filtre les issues "wrong_template" (non corrigeables)
       │    └─ Regroupe les issues restantes par slideIndex
       │
       ├─ writeSlides(indices corrigés, feedback par slide)
       │    └─ Les Writers reçoivent le feedback dans une section
       │       "CORRECTIONS DEMANDÉES" ajoutée au prompt
       │
       └─ runReviewerSubset(indices corrigés)
            └─ Re-review ciblé uniquement sur les slides modifiées
```

La boucle s'exécute jusqu'à `MaxReviewRetries` itérations (défaut : 2). Si le Reviewer n'approuve toujours pas après les retries, le pipeline continue avec le plan courant et un warning.

## Suivi des issues

L'`IssueLog` accumule les `IssueRecord` à travers les itérations :

```go
type IssueRecord struct {
    Agent     string
    Iteration int
    Issues    []ReviewIssue
    Resolved  bool
}
```

Les issues résolues lors d'un passage ultérieur sont marquées `Resolved: true`. Ce log est ensuite utilisé par le [système de mémoire](./agent-memory.md) pour synthétiser des guidelines à partir des erreurs récurrentes.

## Voir aussi

- [PipelineState](./pipeline-state.md) — l'état modifié par la boucle
- [Validation](./validation.md) — validations programmatiques entre étapes
