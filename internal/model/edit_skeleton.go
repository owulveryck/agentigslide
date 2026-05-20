package model

// EditSkeleton is the intermediate representation between the EditPlanner
// (structural decisions) and the EditWriters (text generation). It contains
// intentions instead of final text.
type EditSkeleton struct {
	PresentationID string              `json:"presentationId"`
	Operations     []SkeletonOperation `json:"operations"`
}

// SkeletonOperation describes a single edit operation with intentions
// instead of final text content.
type SkeletonOperation struct {
	Type           string               `json:"type"`
	SlideIndex     int                  `json:"slideIndex"`
	Rationale      string               `json:"rationale"`
	Modifications  []ModificationIntent `json:"modifications,omitempty"`
	NewSourceSlide int                  `json:"newSourceSlide,omitempty"`
	InsertPosition int                  `json:"insertPosition,omitempty"`
	Intention      string               `json:"intention,omitempty"`
	ContentIntents []ContentIntent      `json:"contentIntents,omitempty"`
}

// ModificationIntent describes what should change in an existing text element,
// without providing the actual new text. The EditWriter agent uses this to
// generate the final text.
type ModificationIntent struct {
	VariableName string `json:"variableName"`
	CurrentText  string `json:"currentText"`
	Intention    string `json:"intention"`
}

// ContentIntent describes the intended content for a field in a new or
// replaced slide. Used for replace_slide and insert_slide operations.
type ContentIntent struct {
	VariableName string `json:"variableName"`
	Intention    string `json:"intention"`
}
