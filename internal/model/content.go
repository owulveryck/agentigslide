package model

// SlideContent represents the raw content of a single Google Slides page,
// including its object ID and all page elements (shapes, images, tables, groups).
type SlideContent struct {
	ObjectID     string        `json:"objectId"`
	PageElements []PageElement `json:"pageElements"`
}

// PageElement represents a single element on a slide page. It may be a
// shape, image, table, or element group, along with optional size and
// transform properties for positioning.
type PageElement struct {
	ObjectID     string        `json:"objectId"`
	Shape        *Shape        `json:"shape,omitempty"`
	Image        *Image        `json:"image,omitempty"`
	Table        *Table        `json:"table,omitempty"`
	ElementGroup *ElementGroup `json:"elementGroup,omitempty"`
	Size         *Size         `json:"size,omitempty"`
	Transform    *Transform    `json:"transform,omitempty"`
}

// Image represents an image element with its content URL.
type Image struct {
	ContentURL string `json:"contentUrl,omitempty"`
}

// Table represents a table element with its row and column counts and cell data.
type Table struct {
	Rows      int        `json:"rows"`
	Columns   int        `json:"columns"`
	TableRows []TableRow `json:"tableRows,omitempty"`
}

// TableRow represents a single row in a table, containing its cells.
type TableRow struct {
	TableCells []TableCell `json:"tableCells,omitempty"`
}

// TableCell represents a single cell in a table, optionally containing text.
type TableCell struct {
	Text *TextContent `json:"text,omitempty"`
}

// TextContent holds the text elements within a shape or table cell.
type TextContent struct {
	TextElements []TextElement `json:"textElements,omitempty"`
}

// TextElement represents a single text element, which may contain a text run.
type TextElement struct {
	TextRun *TextRun `json:"textRun,omitempty"`
}

// TextRun represents a contiguous run of text with uniform styling.
type TextRun struct {
	Content string        `json:"content"`
	Style   *TextRunStyle `json:"style,omitempty"`
}

// TextRunStyle contains styling information for a text run, such as font size.
type TextRunStyle struct {
	FontSize   *Magnitude `json:"fontSize,omitempty"`
	FontFamily string     `json:"fontFamily,omitempty"`
	Bold       bool       `json:"bold,omitempty"`
}

// Shape represents a shape element that may contain text and act as a placeholder.
type Shape struct {
	ShapeType   string       `json:"shapeType,omitempty"`
	Text        *TextContent `json:"text,omitempty"`
	Placeholder *Placeholder `json:"placeholder,omitempty"`
}

// Placeholder identifies a shape as a placeholder with a specific type
// (e.g., "TITLE", "BODY") and index within the slide layout.
type Placeholder struct {
	Type  string `json:"type"`
	Index int    `json:"index,omitempty"`
}

// ElementGroup represents a group of page elements that are treated as a
// single unit for positioning and transformation purposes.
type ElementGroup struct {
	Children []PageElement `json:"children,omitempty"`
}

// Size represents the dimensions of a page element as height and width magnitudes.
type Size struct {
	Height Magnitude `json:"height"`
	Width  Magnitude `json:"width"`
}

// Magnitude represents a numerical value with a unit (e.g., EMU or PT).
type Magnitude struct {
	Magnitude float64 `json:"magnitude"`
	Unit      string  `json:"unit,omitempty"`
}

// Transform describes the affine transformation applied to a page element,
// including translation (position) and scale factors.
type Transform struct {
	TranslateX float64 `json:"translateX"`
	TranslateY float64 `json:"translateY"`
	ScaleX     float64 `json:"scaleX,omitempty"`
	ScaleY     float64 `json:"scaleY,omitempty"`
	Unit       string  `json:"unit,omitempty"`
}
