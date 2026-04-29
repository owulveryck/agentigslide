package model

// TemplateIndex is the top-level searchable index of all template slides.
// It associates a template presentation ID with the list of analyzed slides
// available for selection during presentation generation.
type TemplateIndex struct {
	TemplateID string          `json:"templateId"`
	Slides     []TemplateSlide `json:"slides"`
}

// TemplateSlide holds metadata for a single template slide, including its
// intended purpose, search keywords, and inventories of editable fields
// and visual elements.
type TemplateSlide struct {
	SlideNumber    int                    `json:"slideNumber"`
	SlideID        string                 `json:"slideId"`
	Intention      string                 `json:"intention"`
	Keywords       []string               `json:"keywords"`
	EditableFields []EditableFieldSummary `json:"editableFields"`
	VisualElements []VisualElementSummary `json:"visualElements,omitempty"`
}

// EditableFieldSummary provides a compact summary of an editable field
// for the template index. It includes the field's semantic variable name,
// role, dimensions in points, and estimated maximum character capacity.
type EditableFieldSummary struct {
	ObjectID     string        `json:"objectId"`
	Role         string        `json:"role"`
	Placeholder  *string       `json:"placeholder"`
	Content      string        `json:"content,omitempty"`
	RawContent   string        `json:"rawContent,omitempty"`
	VariableName string        `json:"variableName"`
	CellLocation *CellLocation `json:"cellLocation,omitempty"`
	WidthPt      float64       `json:"widthPt,omitempty"`
	HeightPt     float64       `json:"heightPt,omitempty"`
	MaxChars     int           `json:"maxChars,omitempty"`
}

// VisualElementSummary provides a compact summary of a visual element
// for the template index, including its type and purpose.
type VisualElementSummary struct {
	ObjectID *string `json:"objectId,omitempty"`
	Type     string  `json:"type"`
	Purpose  string  `json:"purpose,omitempty"`
}
