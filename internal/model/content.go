package model

type SlideContent struct {
	ObjectID     string        `json:"objectId"`
	PageElements []PageElement `json:"pageElements"`
}

type PageElement struct {
	ObjectID     string        `json:"objectId"`
	Shape        *Shape        `json:"shape,omitempty"`
	Image        *Image        `json:"image,omitempty"`
	Table        *Table        `json:"table,omitempty"`
	ElementGroup *ElementGroup `json:"elementGroup,omitempty"`
	Size         *Size         `json:"size,omitempty"`
	Transform    *Transform    `json:"transform,omitempty"`
}

type Image struct {
	ContentURL string `json:"contentUrl,omitempty"`
}

type Table struct {
	Rows      int        `json:"rows"`
	Columns   int        `json:"columns"`
	TableRows []TableRow `json:"tableRows,omitempty"`
}

type TableRow struct {
	TableCells []TableCell `json:"tableCells,omitempty"`
}

type TableCell struct {
	Text *TextContent `json:"text,omitempty"`
}

type TextContent struct {
	TextElements []TextElement `json:"textElements,omitempty"`
}

type TextElement struct {
	TextRun *TextRun `json:"textRun,omitempty"`
}

type TextRun struct {
	Content string        `json:"content"`
	Style   *TextRunStyle `json:"style,omitempty"`
}

type TextRunStyle struct {
	FontSize *Magnitude `json:"fontSize,omitempty"`
}

type Shape struct {
	ShapeType   string       `json:"shapeType,omitempty"`
	Text        *TextContent `json:"text,omitempty"`
	Placeholder *Placeholder `json:"placeholder,omitempty"`
}

type Placeholder struct {
	Type  string `json:"type"`
	Index int    `json:"index,omitempty"`
}

type ElementGroup struct {
	Children []PageElement `json:"children,omitempty"`
}

type Size struct {
	Height Magnitude `json:"height"`
	Width  Magnitude `json:"width"`
}

type Magnitude struct {
	Magnitude float64 `json:"magnitude"`
	Unit      string  `json:"unit,omitempty"`
}

type Transform struct {
	TranslateX float64 `json:"translateX"`
	TranslateY float64 `json:"translateY"`
	ScaleX     float64 `json:"scaleX,omitempty"`
	ScaleY     float64 `json:"scaleY,omitempty"`
	Unit       string  `json:"unit,omitempty"`
}
