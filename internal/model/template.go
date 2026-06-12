package model

// IsContentField reports whether a field role represents user-editable content
// as opposed to metadata fields like year or copyright. It lives in model (the
// lowest layer) so both plan and templateindex can share it without an import
// cycle.
func IsContentField(role string) bool {
	switch role {
	case "annee", "copyright", "entreprise":
		return false
	}
	return true
}

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
	SlideNumber       int                    `json:"slideNumber"`
	SlideID           string                 `json:"slideId"`
	Intention         string                 `json:"intention"`
	Description       string                 `json:"description,omitempty"`
	Category          string                 `json:"category,omitempty"`
	UseCaseTags       []string               `json:"useCaseTags,omitempty"`
	VisualStyle       string                 `json:"visualStyle,omitempty"`
	VisualCaveats     []string               `json:"visualCaveats,omitempty"`
	LayoutDescription string                 `json:"layoutDescription,omitempty"`
	Keywords          []string               `json:"keywords"`
	EditableFields    []EditableFieldSummary `json:"editableFields"`
	VisualElements    []VisualElementSummary `json:"visualElements,omitempty"`
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
	// CharsPerLine and Lines expose the box geometry behind MaxChars so
	// writers can shape text for the box (e.g. avoid words longer than a
	// line) instead of only counting characters.
	CharsPerLine int `json:"charsPerLine,omitempty"`
	Lines        int `json:"lines,omitempty"`
}

// VisualElementSummary provides a compact summary of a visual element
// for the template index, including its type and purpose.
type VisualElementSummary struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description,omitempty"`
	Purpose     string  `json:"purpose,omitempty"`
	Reusable    bool    `json:"reusable,omitempty"`
}
