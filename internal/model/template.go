package model

type TemplateIndex struct {
	TemplateID string          `json:"templateId"`
	Slides     []TemplateSlide `json:"slides"`
}

type TemplateSlide struct {
	SlideNumber    int                    `json:"slideNumber"`
	SlideID        string                 `json:"slideId"`
	Intention      string                 `json:"intention"`
	Keywords       []string               `json:"keywords"`
	EditableFields []EditableFieldSummary `json:"editableFields"`
	VisualElements []VisualElementSummary `json:"visualElements,omitempty"`
}

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

type VisualElementSummary struct {
	ObjectID *string `json:"objectId,omitempty"`
	Type     string  `json:"type"`
	Purpose  string  `json:"purpose,omitempty"`
}
