package agent

import (
	"sync"

	"github.com/owulveryck/slideAppScripter/internal/model"
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
	SlideType     string   `json:"slideType"`
}

// SelectionPlan is the output of the Selector agent. It maps each SlideNeed
// to a concrete template slide with field assignments.
type SelectionPlan struct {
	Selections []SlideSelection `json:"selections"`
}

// SlideSelection maps a single SlideNeed to a template slide.
type SlideSelection struct {
	OutlineIndex int               `json:"outlineIndex"`
	SourceSlide  int               `json:"sourceSlide"`
	Rationale    string            `json:"rationale"`
	FieldMapping []FieldAssignment `json:"fieldMapping"`
}

// FieldAssignment maps a template field to a content item from the outline.
type FieldAssignment struct {
	VariableName string `json:"variableName"`
	ContentIndex int    `json:"contentIndex"`
	MaxChars     int    `json:"maxChars"`
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

// PipelineState holds the shared mutable state passed between agents via the
// orchestrator. Writers access it concurrently; all other agents run
// sequentially.
type PipelineState struct {
	mu sync.Mutex

	UserRequest    string
	CompactCatalog string

	Outline       *PresentationOutline
	Selections    *SelectionPlan
	SlideContents []SlideContent
	AssembledPlan *model.GenerationPlan
	ReviewResult  *ReviewResult
}

// SetSlideContent safely sets the content for a specific index in SlideContents.
func (s *PipelineState) SetSlideContent(index int, content SlideContent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SlideContents[index] = content
}
