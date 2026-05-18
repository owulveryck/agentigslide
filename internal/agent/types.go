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

// DiagramSpec describes the topology of a diagram to be rendered on a slide.
// The agent outputs this structure; layout computation happens in Go.
type DiagramSpec struct {
	Title      string         `json:"title,omitempty"`
	LayoutHint string         `json:"layoutHint"`
	Nodes      []DiagramNode  `json:"nodes"`
	Edges      []DiagramEdge  `json:"edges"`
	Groups     []DiagramGroup `json:"groups,omitempty"`
}

// DiagramNode represents a single shape in a diagram.
type DiagramNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Shape string `json:"shape,omitempty"`
	Style string `json:"style,omitempty"`
}

// DiagramEdge represents a connection between two nodes.
type DiagramEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label,omitempty"`
	LineStyle string `json:"lineStyle,omitempty"`
}

// DiagramGroup represents a visual zone grouping several nodes.
type DiagramGroup struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Nodes []string `json:"nodes"`
	Style string   `json:"style,omitempty"`
}

// PipelineState holds the shared mutable state passed between agents via the
// orchestrator. Writers and Designers access it concurrently; all other agents
// run sequentially.
type PipelineState struct {
	mu sync.Mutex

	UserRequest          string
	CompactCatalog       string
	TemplateInstructions string

	Outline       *PresentationOutline
	Selections    *SelectionPlan
	SlideContents []SlideContent
	DiagramSpecs  map[int]*DiagramSpec
	AssembledPlan *model.GenerationPlan
	ReviewResult  *ReviewResult
}

// SetSlideContent safely sets the content for a specific index in SlideContents.
func (s *PipelineState) SetSlideContent(index int, content SlideContent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SlideContents[index] = content
}

// SetDiagramSpec safely sets the diagram spec for a specific selection index.
func (s *PipelineState) SetDiagramSpec(index int, spec *DiagramSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DiagramSpecs == nil {
		s.DiagramSpecs = make(map[int]*DiagramSpec)
	}
	s.DiagramSpecs[index] = spec
}
