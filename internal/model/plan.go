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
// the editable text objects and visual objects to include.
type SlideSpec struct {
	Position          int              `json:"position"`
	SourceSlideNumber int              `json:"sourceSlideNumber"`
	SourceSlideID     string           `json:"sourceSlideId"`
	Intention         string           `json:"intention"`
	Description       string           `json:"description"`
	PreviewImage      string           `json:"previewImage"`
	EditableObjects   []EditableObject `json:"editableObjects"`
	VisualObjects     []VisualObject   `json:"visualObjects,omitempty"`
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
// which template slide to use and what text modifications to apply.
type SlideRequest struct {
	SourceSlide   int                `json:"sourceSlide"`
	Modifications []TextModification `json:"modifications"`
}

// TextModification maps a semantic variable name to the new text content
// that should replace the current value in the corresponding slide element.
type TextModification struct {
	VariableName string `json:"variableName"`
	NewText      string `json:"newText"`
}
