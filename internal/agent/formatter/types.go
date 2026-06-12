package formatter

// RGBColor represents a color as RGB components in the 0.0–1.0 range.
type RGBColor struct {
	Red   float64 `json:"red"`
	Green float64 `json:"green"`
	Blue  float64 `json:"blue"`
}

// SlideInfo holds structural information about a single slide, including its
// index, page ID, and all text-bearing elements found on the slide.
type SlideInfo struct {
	SlideIndex int           `json:"slideIndex"`
	PageID     string        `json:"pageId"`
	Elements   []ElementInfo `json:"elements"`
}

// ElementInfo contains detailed information about a text-bearing element,
// including its object ID, shape type, bounding box, text runs with styling,
// and paragraph spacing information.
type ElementInfo struct {
	ObjectID         string          `json:"objectId"`
	ShapeType        string          `json:"shapeType,omitempty"`
	PlaceholderType  string          `json:"placeholderType,omitempty"`
	BoundingBox      BoundingBox     `json:"boundingBox"`
	TextRuns         []TextRunInfo   `json:"textRuns"`
	Paragraphs       []ParagraphInfo `json:"paragraphs"`
	CellLocation     *CellRef        `json:"cellLocation,omitempty"`
	BackgroundColor  *RGBColor       `json:"backgroundColor,omitempty"`
	ContentAlignment string          `json:"contentAlignment,omitempty"`
	OutlineColor     *RGBColor       `json:"outlineColor,omitempty"`
	OutlineWeightPt  float64         `json:"outlineWeightPt,omitempty"`
}

// BoundingBox represents the position and dimensions of an element in points.
type BoundingBox struct {
	WidthPt  float64 `json:"widthPt"`
	HeightPt float64 `json:"heightPt"`
	LeftPt   float64 `json:"leftPt"`
	TopPt    float64 `json:"topPt"`
}

// TextRunInfo holds the style and content information for a single text run.
type TextRunInfo struct {
	StartIndex      int       `json:"startIndex"`
	EndIndex        int       `json:"endIndex"`
	Content         string    `json:"content"`
	FontFamily      string    `json:"fontFamily,omitempty"`
	FontSizePt      float64   `json:"fontSizePt,omitempty"`
	Bold            bool      `json:"bold,omitempty"`
	Italic          bool      `json:"italic,omitempty"`
	Underline       bool      `json:"underline,omitempty"`
	Strikethrough   bool      `json:"strikethrough,omitempty"`
	ForegroundColor *RGBColor `json:"foregroundColor,omitempty"`
}

// ParagraphInfo holds the spacing and alignment information for a single paragraph.
type ParagraphInfo struct {
	StartIndex    int     `json:"startIndex"`
	EndIndex      int     `json:"endIndex"`
	LineSpacing   float64 `json:"lineSpacing,omitempty"`
	SpaceAbovePt  float64 `json:"spaceAbovePt,omitempty"`
	SpaceBelowPt  float64 `json:"spaceBelowPt,omitempty"`
	Alignment     string  `json:"alignment,omitempty"`
	IndentStartPt float64 `json:"indentStartPt,omitempty"`
	IndentEndPt   float64 `json:"indentEndPt,omitempty"`
	IndentFirstPt float64 `json:"indentFirstPt,omitempty"`
}

// CellRef identifies a specific table cell by its row and column indices.
type CellRef struct {
	RowIndex    int `json:"rowIndex"`
	ColumnIndex int `json:"columnIndex"`
}

// CorrectionPlan holds the set of formatting corrections.
type CorrectionPlan struct {
	Corrections []Correction `json:"corrections"`
}

// Correction describes a single formatting correction to apply.
type Correction struct {
	ObjectID     string   `json:"objectId"`
	SlideIndex   int      `json:"slideIndex"`
	CellLocation *CellRef `json:"cellLocation,omitempty"`
	Reason       string   `json:"reason"`
	Type         string   `json:"type"` // "textStyle", "paragraphStyle", or "shapeProperties"

	// textStyle fields
	StartIndex      *int      `json:"startIndex,omitempty"`
	EndIndex        *int      `json:"endIndex,omitempty"`
	FontSizePt      *float64  `json:"fontSizePt,omitempty"`
	FontFamily      *string   `json:"fontFamily,omitempty"`
	ForegroundColor *RGBColor `json:"foregroundColor,omitempty"`
	Bold            *bool     `json:"bold,omitempty"`
	Italic          *bool     `json:"italic,omitempty"`
	Underline       *bool     `json:"underline,omitempty"`
	Strikethrough   *bool     `json:"strikethrough,omitempty"`

	// paragraphStyle fields
	LineSpacing  *float64 `json:"lineSpacing,omitempty"`
	SpaceAbovePt *float64 `json:"spaceAbovePt,omitempty"`
	SpaceBelowPt *float64 `json:"spaceBelowPt,omitempty"`
	Alignment    *string  `json:"alignment,omitempty"`

	// shapeProperties fields
	BackgroundColor     *RGBColor `json:"backgroundColor,omitempty"`
	ContentAlignmentVal *string   `json:"contentAlignment,omitempty"`
	OutlineColor        *RGBColor `json:"outlineColor,omitempty"`
	OutlineWeightPt     *float64  `json:"outlineWeightPt,omitempty"`
}

// ConsistencyIssue describes a formatting inconsistency detected by the
// deterministic consistency checker.
type ConsistencyIssue struct {
	Rule       string `json:"rule"`
	SlideIndex int    `json:"slideIndex"`
	ObjectID   string `json:"objectId"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Severity   string `json:"severity"` // "error" or "warning"
}

// FormatterResult holds the output of the Formatter agent.
type FormatterResult struct {
	Issues       []ConsistencyIssue `json:"issues"`
	Corrections  []Correction       `json:"corrections"`
	AppliedCount int                `json:"appliedCount"`
}
