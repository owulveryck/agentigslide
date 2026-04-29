package model

type SlideAnalysis struct {
	SlideNumber      int               `json:"slideNumber"`
	SlideID          string            `json:"slideId"`
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []EditableElement `json:"editableElements"`
	VisualElements   []VisualElement   `json:"visualElements"`
}

type EditableElement struct {
	ObjectID    string  `json:"objectId"`
	Type        string  `json:"type"`
	Placeholder *string `json:"placeholder"`
	Content     string  `json:"content"`
	Description string  `json:"description"`
	Location    string  `json:"location"`
}

type VisualElement struct {
	ObjectID    *string `json:"objectId,omitempty"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Purpose     string  `json:"purpose,omitempty"`
	Reusable    bool    `json:"reusable,omitempty"`
}

type VisionResponse struct {
	Intention        string            `json:"intention"`
	Description      string            `json:"description"`
	EditableElements []EditableElement `json:"editableElements"`
	VisualElements   []VisualElement   `json:"visualElements"`
}
