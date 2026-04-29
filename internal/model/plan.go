package model

type PresentationPlan struct {
	PresentationTitle string      `json:"presentationTitle"`
	TemplateID        string      `json:"templateId"`
	GeneratedAt       string      `json:"generatedAt"`
	SourceRequest     string      `json:"sourceRequest"`
	Slides            []SlideSpec `json:"slides"`
}

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

type CellLocation struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

type VisualObject struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose"`
	Reusable    bool    `json:"reusable"`
}

type GenerationPlan struct {
	PresentationTitle string         `json:"presentationTitle"`
	Slides            []SlideRequest `json:"slides"`
}

type SlideRequest struct {
	SourceSlide   int                `json:"sourceSlide"`
	Modifications []TextModification `json:"modifications"`
}

type TextModification struct {
	VariableName string `json:"variableName"`
	NewText      string `json:"newText"`
}
