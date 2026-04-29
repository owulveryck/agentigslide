package model

// SlideRef maps a duplicated slide's page object ID to a remapping of its
// original element IDs. The ElementMap keys are original ObjectIDs and the
// values are the new ObjectIDs assigned during the DuplicateObject operation.
type SlideRef struct {
	PageObjectID string
	ElementMap   map[string]string
}
