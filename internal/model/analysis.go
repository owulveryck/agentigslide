package model

// SlideAnalysis represents the result of a Claude Vision analysis of a single
// slide. It captures the slide's intended purpose, a human-readable description,
// and inventories of both editable text elements and visual components.
type SlideAnalysis struct {
	SlideNumber      int               `json:"slideNumber"`
	SlideID          string            `json:"slideId"`
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []EditableElement `json:"editableElements"`
	VisualElements   []VisualElement   `json:"visualElements"`
}

// EditableElement describes a text element in a slide that can be modified.
// It includes the element's Google Slides ObjectID, its type (e.g., "text",
// "title"), placeholder text, current content, and spatial location.
type EditableElement struct {
	ObjectID    string  `json:"objectId"`
	Type        string  `json:"type"`
	Placeholder *string `json:"placeholder"`
	Content     string  `json:"content"`
	Description string  `json:"description"`
	Location    string  `json:"location"`
}

// VisualElement describes a visual component (image, icon, logo, or decoration)
// identified in a slide. It indicates whether the element is reusable across
// different presentations and may include a Google Slides ObjectID for copying.
type VisualElement struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose,omitempty"`
	Reusable    bool    `json:"reusable,omitempty"`
}

// VisionResponse is the raw JSON structure returned by Claude Vision when
// analyzing a slide image. It is parsed into a SlideAnalysis after adding
// slide number and ID metadata.
type VisionResponse struct {
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []EditableElement `json:"editableElements"`
	VisualElements   []VisualElement   `json:"visualElements"`
}
