# PipelineState

`PipelineState` est la structure centrale qui porte l'état partagé du pipeline multi-agents. Elle est définie dans [`internal/agent/types.go`](../internal/agent/types.go).

## Structure

```go
type PipelineState struct {
    mu sync.Mutex

    // Entrées (immutables pendant le pipeline)
    UserRequest          string
    CompactCatalog       string
    TemplateInstructions string
    AgentMemories        map[string]string

    // Sorties (remplies progressivement par les agents)
    Outline       *PresentationOutline
    Selections    *SelectionPlan
    SlideContents []SlideContent
    DiagramSpecs  map[int]*model.DiagramSpec
    AssembledPlan *model.GenerationPlan
    ReviewResult  *ReviewResult
    Issues        IssueLog
}
```

## Cycle de vie

Les champs d'entrée sont initialisés avant le lancement du pipeline et restent constants. Les champs de sortie sont remplis séquentiellement par chaque étape :

```
Outliner  → state.Outline
Selector  → state.Selections
Writers   → state.SlideContents[i], state.DiagramSpecs[i]
Assembler → state.AssembledPlan
Reviewer  → state.ReviewResult, state.Issues
```

## Accès concurrent

Les Writers s'exécutent en parallèle (goroutines contrôlées par un sémaphore `MaxParallel`). Chacun écrit dans un index différent de `SlideContents` ou `DiagramSpecs`. Le mutex protège ces écritures :

```go
func (s *PipelineState) SetSlideContent(index int, content SlideContent) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.SlideContents[index] = content
}

func (s *PipelineState) SetDiagramSpec(index int, spec *model.DiagramSpec) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.DiagramSpecs == nil {
        s.DiagramSpecs = make(map[int]*model.DiagramSpec)
    }
    s.DiagramSpecs[index] = spec
}
```

Les autres agents (Outliner, Selector, Reviewer) s'exécutent séquentiellement et n'ont pas besoin du mutex.

## Voir aussi

- [Boucle de feedback (ReviewIssue)](./review-feedback-loop.md) — comment le Reviewer modifie l'état
- [Métriques](./metrics.md) — observabilité du pipeline
