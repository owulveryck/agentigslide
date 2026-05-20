package model

// ExistingSlideInfo describes a slide in an existing presentation as read
// from the Google Slides API structural data.
type ExistingSlideInfo struct {
	Index        int            `json:"index"`
	PageObjectID string         `json:"pageObjectId"`
	TextElements []ExistingText `json:"textElements"`
}

// ExistingText describes a text-bearing element in an existing slide.
type ExistingText struct {
	ObjectID     string        `json:"objectId"`
	Content      string        `json:"content"`
	ShapeType    string        `json:"shapeType"`
	CellLocation *CellLocation `json:"cellLocation,omitempty"`
}

// EditPlan describes a set of modifications to apply to an existing presentation.
type EditPlan struct {
	PresentationID string          `json:"presentationId"`
	Operations     []EditOperation `json:"operations"`
}

// EditOperation describes a single operation on an existing presentation.
// Type is one of: modify_content, replace_slide, insert_slide, delete_slide.
type EditOperation struct {
	Type           string             `json:"type"`
	SlideIndex     int                `json:"slideIndex"`
	Modifications  []TextModification `json:"modifications,omitempty"`
	NewSourceSlide int                `json:"newSourceSlide,omitempty"`
	SlideContent   []TextModification `json:"slideContent,omitempty"`
	InsertPosition int                `json:"insertPosition,omitempty"`
	Intention      string             `json:"intention,omitempty"`
	Rationale      string             `json:"rationale"`
}
