package agent

import (
	"sync"

	"github.com/owulveryck/agentigslide/internal/model"
)

// PresentationOutline is the output of the Outliner agent. It describes the
// logical structure of the presentation independently of available templates.
type PresentationOutline struct {
	PresentationTitle string        `json:"presentationTitle"`
	Sections          []SectionSpec `json:"sections"`
}

// SectionSpec describes a logical section of the presentation.
type SectionSpec struct {
	Title      string      `json:"title"`
	Purpose    string      `json:"purpose"`
	SlideNeeds []SlideNeed `json:"slideNeeds"`
}

// SlideNeed describes what a single slide should convey, with the content
// items extracted from the user request.
type SlideNeed struct {
	Intent        string   `json:"intent"`
	ContentItems  []string `json:"contentItems"`
	ItemCount     int      `json:"itemCount"`
	MaxItemLength int      `json:"maxItemLength"`
	NeedsTitle    bool     `json:"needsTitle"`
	NeedsSubtitle bool     `json:"needsSubtitle"`
	SlideType     string   `json:"slideType"`
}

// SelectionPlan is the output of the Selector agent. It maps each SlideNeed
// to a concrete template slide with field assignments.
type SelectionPlan struct {
	Selections []SlideSelection `json:"selections"`
}

// SlideSelection maps a single SlideNeed to a template slide.
type SlideSelection struct {
	OutlineIndex int    `json:"outlineIndex"`
	SourceSlide  int    `json:"sourceSlide"`
	Rationale    string `json:"rationale"`
}

// TemplateField describes a single editable field in a template slide, as
// parsed from the compact catalog.
type TemplateField struct {
	VariableName string
	Role         string
	MaxChars     int
}

// SlideContent is the output of a single Writer agent invocation. It contains
// the text modifications for one slide.
type SlideContent struct {
	SourceSlide   int                      `json:"sourceSlide"`
	Modifications []model.TextModification `json:"modifications"`
}

// ReviewResult is the output of the Reviewer agent.
type ReviewResult struct {
	Approved bool          `json:"approved"`
	Issues   []ReviewIssue `json:"issues"`
}

// ReviewIssue describes a single quality problem found by the Reviewer.
type ReviewIssue struct {
	SlideIndex  int    `json:"slideIndex"`
	Field       string `json:"field,omitempty"`
	IssueType   string `json:"issueType"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// IssueRecord captures all issues detected during a single pass of an agent,
// along with whether they were resolved in a subsequent iteration.
type IssueRecord struct {
	Agent     string        `json:"agent"`
	Iteration int           `json:"iteration"`
	Issues    []ReviewIssue `json:"issues"`
	Resolved  bool          `json:"resolved"`
}

// IssueLog accumulates IssueRecords across all retry iterations of a pipeline
// run. It is used at the end of the pipeline to synthesize agent memory.
type IssueLog []IssueRecord

// Record appends an IssueRecord to the log.
func (l *IssueLog) Record(agent string, iteration int, issues []ReviewIssue) {
	if len(issues) == 0 {
		return
	}
	*l = append(*l, IssueRecord{Agent: agent, Iteration: iteration, Issues: issues})
}

// MarkResolved marks all records for the given agent at the given iteration as
// resolved (the issues were fixed in a subsequent pass).
func (l *IssueLog) MarkResolved(agent string, iteration int) {
	for i := range *l {
		if (*l)[i].Agent == agent && (*l)[i].Iteration == iteration {
			(*l)[i].Resolved = true
		}
	}
}

// HasIssues returns true if the log contains any issue records.
func (l *IssueLog) HasIssues() bool {
	return len(*l) > 0
}

// PipelineState holds the shared mutable state passed between agents via the
// orchestrator. Writers and Designers access it concurrently; all other agents
// run sequentially.
type PipelineState struct {
	mu sync.Mutex

	UserRequest          string
	CompactCatalog       string
	TemplateInstructions string
	AgentMemories        map[string]string

	Outline       *PresentationOutline
	Selections    *SelectionPlan
	SlideContents []SlideContent
	DiagramSpecs  map[int]*model.DiagramSpec
	AssembledPlan *model.GenerationPlan
	ReviewResult  *ReviewResult
	Issues        IssueLog
}

// SetSlideContent safely sets the content for a specific index in SlideContents.
func (s *PipelineState) SetSlideContent(index int, content SlideContent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SlideContents[index] = content
}

// SetDiagramSpec safely sets the diagram spec for a specific selection index.
func (s *PipelineState) SetDiagramSpec(index int, spec *model.DiagramSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DiagramSpecs == nil {
		s.DiagramSpecs = make(map[int]*model.DiagramSpec)
	}
	s.DiagramSpecs[index] = spec
}
