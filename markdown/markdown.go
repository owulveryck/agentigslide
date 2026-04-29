package markdown

import (
	"sort"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"google.golang.org/api/slides/v1"
)

type Chunk struct {
	Content          string
	Style            Style
	IndentationLevel int
	Paragraph        int
}

type Style []byte

const (
	NormalMask byte = 1 << 0
	BoldMask   byte = 1 << 1
	ItalicMask byte = 1 << 2
)

func EncodeStyle(bold, italic bool) Style {
	var s byte
	if bold {
		s |= BoldMask
	}
	if italic {
		s |= ItalicMask
	}
	if !italic && !bold {
		s |= NormalMask
	}
	return Style{s}
}

func DecodeStyle(s Style) (bold, italic, normal bool) {
	if len(s) == 0 {
		return false, false, false
	}
	bold = s[0]&BoldMask != 0
	italic = s[0]&ItalicMask != 0
	normal = s[0]&NormalMask != 0
	return
}

type parser struct {
	paragraph int
}

func (p *parser) processNode(n ast.Node, reader text.Reader, level int, currentStyle Style, chunks *[]Chunk) {
	switch n.Kind() {
	case ast.KindEmphasis:
	case ast.KindText:
	case ast.KindList:
		p.paragraph++
	case ast.KindListItem:
		p.paragraph++
	case ast.KindParagraph:
		p.paragraph++
	case ast.KindTextBlock:
		p.paragraph++
	case ast.KindDocument:
	}

	if textNode, ok := n.(*ast.Text); ok {
		content := textNode.Segment.Value(reader.Source())
		*chunks = append(*chunks, Chunk{
			Content:          string(content),
			Style:            currentStyle,
			IndentationLevel: level,
			Paragraph:        p.paragraph,
		})
	}

	if emphasisNode, ok := n.(*ast.Emphasis); ok {
		bold, italic, _ := DecodeStyle(currentStyle)
		switch emphasisNode.Level {
		case 1:
			italic = true
		case 2:
			bold = true
		}
		newStyle := EncodeStyle(bold, italic)
		for child := emphasisNode.FirstChild(); child != nil; child = child.NextSibling() {
			p.processNode(child, reader, level, newStyle, chunks)
		}
		return
	}

	if _, ok := n.(*ast.ListItem); ok {
		level++
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			p.processNode(child, reader, level, currentStyle, chunks)
		}
		return
	}

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		p.processNode(child, reader, level, currentStyle, chunks)
	}
}

func parseContent(input string) []Chunk {
	md := goldmark.New()
	reader := text.NewReader([]byte(input))
	document := md.Parser().Parse(reader)

	p := &parser{}
	var chunks []Chunk
	p.processNode(document, reader, 0, EncodeStyle(false, false), &chunks)
	return chunks
}

// InsertMarkdownContent parses markdown and generates Google Slides API
// requests for text insertion with bold, italic, and bullet formatting.
func InsertMarkdownContent(input string, objectID string) []*slides.Request {
	chunks := parseContent(input)
	var requests []*slides.Request
	currentIndex := int64(0)

	inList := false
	var inListStartIndex, inListEndIndex int64

	for i, c := range chunks {
		if i < len(chunks)-1 && chunks[i+1].Paragraph != c.Paragraph {
			c.Content += "\n"
		}

		if i > 0 {
			if chunks[i-1].Paragraph != c.Paragraph {
				if c.IndentationLevel == 2 {
					c.Content = "\t" + c.Content
				}
			}
		}
		startIndex := currentIndex
		endIndex := startIndex + int64(utf8.RuneCountInString(c.Content))

		requests = append(requests, &slides.Request{
			InsertText: &slides.InsertTextRequest{
				ObjectId:        objectID,
				InsertionIndex:  currentIndex,
				Text:            c.Content,
				ForceSendFields: []string{"InsertionIndex"},
			},
		})

		bold, italic, _ := DecodeStyle(c.Style)
		if bold || italic {
			requests = append(requests, &slides.Request{
				UpdateTextStyle: &slides.UpdateTextStyleRequest{
					ObjectId: objectID,
					TextRange: &slides.Range{
						Type:       "FIXED_RANGE",
						StartIndex: &startIndex,
						EndIndex:   &endIndex,
					},
					Style: &slides.TextStyle{
						Bold:   bold,
						Italic: italic,
					},
					Fields: "bold,italic",
				},
			})
		}

		if c.IndentationLevel > 0 && !inList {
			inListStartIndex = startIndex
			inList = true
		}
		if inList {
			inListEndIndex = endIndex
		}
		currentIndex = endIndex
		if (c.IndentationLevel == 0 || i == len(chunks)-1) && inList {
			start := inListStartIndex
			end := inListEndIndex - 1
			if end < start {
				end = inListEndIndex
			}
			requests = append(requests, &slides.Request{
				CreateParagraphBullets: &slides.CreateParagraphBulletsRequest{
					BulletPreset: "BULLET_DISC_CIRCLE_SQUARE",
					ObjectId:     objectID,
					TextRange: &slides.Range{
						StartIndex: &start,
						EndIndex:   &end,
						Type:       "FIXED_RANGE",
					},
				},
			})
			inList = false
		}
	}

	return requests
}

// InsertMarkdownContentInCell is like InsertMarkdownContent but targets a specific table cell.
func InsertMarkdownContentInCell(input string, objectID string, cellLocation *slides.TableCellLocation) []*slides.Request {
	chunks := parseContent(input)
	var requests []*slides.Request
	currentIndex := int64(0)

	inList := false
	var inListStartIndex, inListEndIndex int64

	for i, c := range chunks {
		if i < len(chunks)-1 && chunks[i+1].Paragraph != c.Paragraph {
			c.Content += "\n"
		}

		if i > 0 {
			if chunks[i-1].Paragraph != c.Paragraph {
				if c.IndentationLevel == 2 {
					c.Content = "\t" + c.Content
				}
			}
		}
		startIndex := currentIndex
		endIndex := startIndex + int64(utf8.RuneCountInString(c.Content))

		requests = append(requests, &slides.Request{
			InsertText: &slides.InsertTextRequest{
				ObjectId:        objectID,
				CellLocation:    cellLocation,
				InsertionIndex:  currentIndex,
				Text:            c.Content,
				ForceSendFields: []string{"InsertionIndex"},
			},
		})

		bold, italic, _ := DecodeStyle(c.Style)
		if bold || italic {
			requests = append(requests, &slides.Request{
				UpdateTextStyle: &slides.UpdateTextStyleRequest{
					ObjectId:     objectID,
					CellLocation: cellLocation,
					TextRange: &slides.Range{
						Type:       "FIXED_RANGE",
						StartIndex: &startIndex,
						EndIndex:   &endIndex,
					},
					Style: &slides.TextStyle{
						Bold:   bold,
						Italic: italic,
					},
					Fields: "bold,italic",
				},
			})
		}

		if c.IndentationLevel > 0 && !inList {
			inListStartIndex = startIndex
			inList = true
		}
		if inList {
			inListEndIndex = endIndex
		}
		currentIndex = endIndex
		if (c.IndentationLevel == 0 || i == len(chunks)-1) && inList {
			start := inListStartIndex
			end := inListEndIndex - 1
			if end < start {
				end = inListEndIndex
			}
			requests = append(requests, &slides.Request{
				CreateParagraphBullets: &slides.CreateParagraphBulletsRequest{
					BulletPreset: "BULLET_DISC_CIRCLE_SQUARE",
					ObjectId:     objectID,
					CellLocation: cellLocation,
					TextRange: &slides.Range{
						StartIndex: &start,
						EndIndex:   &end,
						Type:       "FIXED_RANGE",
					},
				},
			})
			inList = false
		}
	}

	return requests
}

// SortRequests orders requests so that deletes run first, then inserts,
// then style updates, then bullet creation.
func SortRequests(requests []*slides.Request) {
	sort.SliceStable(requests, func(i, j int) bool {
		priority := func(req *slides.Request) int {
			switch {
			case req.DeleteText != nil:
				return 0
			case req.InsertText != nil:
				return 1
			case req.UpdateTextStyle != nil:
				return 2
			case req.CreateParagraphBullets != nil:
				return 3
			default:
				return 4
			}
		}
		return priority(requests[i]) < priority(requests[j])
	})
}
