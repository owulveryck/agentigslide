package model

// PresentationPlan represents the complete plan for assembling a presentation.
// It includes the presentation title, the source template ID, generation
// timestamp, the original user request, and the ordered list of slides to create.
type PresentationPlan struct {
	PresentationTitle string      `json:"presentationTitle"`
	TemplateID        string      `json:"templateId"`
	GeneratedAt       string      `json:"generatedAt"`
	SourceRequest     string      `json:"sourceRequest"`
	Slides            []SlideSpec `json:"slides"`
}

// SlideSpec specifies a single slide within a presentation plan. It references
// the source template slide, describes the slide's intended purpose, and lists
// the editable text objects and visual objects to include. For diagram slides,
// the Diagram field is set and the slide is created programmatically.
type SlideSpec struct {
	Position          int              `json:"position"`
	SourceSlideNumber int              `json:"sourceSlideNumber"`
	SourceSlideID     string           `json:"sourceSlideId"`
	Intention         string           `json:"intention"`
	Description       string           `json:"description"`
	PreviewImage      string           `json:"previewImage"`
	EditableObjects   []EditableObject `json:"editableObjects"`
	VisualObjects     []VisualObject   `json:"visualObjects,omitempty"`
	Diagram           *DiagramSpec     `json:"diagram,omitempty"`
}

// EditableObject describes an editable text field in a slide, including its
// Google Slides ObjectID, semantic variable name, role, current value, and
// the new value to set. For table cells, CellLocation specifies the row
// and column indices.
type EditableObject struct {
	ObjectID     string        `json:"objectId"`
	VariableName string        `json:"variableName"`
	Role         string        `json:"role"`
	ElementType  string        `json:"elementType"`
	Placeholder  *string       `json:"placeholder"`
	Description  string        `json:"description"`
	Location     string        `json:"location"`
	CurrentValue string        `json:"currentValue"`
	NewValue     *string       `json:"newValue,omitempty"`
	Modified     bool          `json:"modified"`
	CellLocation *CellLocation `json:"cellLocation,omitempty"`
}

// CellLocation identifies a specific cell in a table by its row and column indices.
type CellLocation struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

// VisualObject describes a visual element (image, icon, or logo) referenced
// in a slide specification, indicating its type, purpose, and whether it
// can be reused across presentations.
type VisualObject struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose"`
	Reusable    bool    `json:"reusable"`
}

// GenerationPlan is Claude's raw output for slide selection and content
// assignment. It contains a presentation title and a list of slide requests
// that reference source template slides by number and specify text modifications.
type GenerationPlan struct {
	PresentationTitle string         `json:"presentationTitle"`
	Slides            []SlideRequest `json:"slides"`
}

// SlideRequest represents a single slide entry in a GenerationPlan, specifying
// which template slide to use and what text modifications to apply. For diagram
// slides, the Diagram field is set instead of Modifications.
type SlideRequest struct {
	SourceSlide   int                `json:"sourceSlide"`
	Modifications []TextModification `json:"modifications"`
	Diagram       *DiagramSpec       `json:"diagram,omitempty"`
}

// DiagramSpec describes the topology of a diagram to be rendered on a slide.
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
	Size  string `json:"size,omitempty"`
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
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Nodes      []string `json:"nodes"`
	Style      string   `json:"style,omitempty"`
	LayoutHint string   `json:"layoutHint,omitempty"`
}

// TextModification maps a semantic variable name to the new text content
// that should replace the current value in the corresponding slide element.
type TextModification struct {
	VariableName string `json:"variableName"`
	NewText      string `json:"newText"`
}
