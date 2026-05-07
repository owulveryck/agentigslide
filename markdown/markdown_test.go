package markdown

import (
	"reflect"
	"testing"
)

func TestInsertMarkdownContentBullets(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"single bullet", "- item"},
		{"two bullets no trailing text", "- alpha\n- beta"},
		{"text then bullets at end", "Hello\n- alpha\n- beta"},
		{"bullets then text", "- alpha\n- beta\n\nEnd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs := InsertMarkdownContent(tt.input, "testObj")
			bulletCount := 0
			for _, r := range reqs {
				if r.CreateParagraphBullets == nil {
					continue
				}
				bulletCount++
				tr := r.CreateParagraphBullets.TextRange
				if tr.StartIndex == nil || tr.EndIndex == nil {
					t.Fatal("bullet request has nil StartIndex or EndIndex")
				}
				if *tr.EndIndex < 0 {
					t.Errorf("EndIndex is negative: %d", *tr.EndIndex)
				}
				if *tr.EndIndex <= *tr.StartIndex {
					t.Errorf("EndIndex (%d) must be > StartIndex (%d)", *tr.EndIndex, *tr.StartIndex)
				}
			}
			if bulletCount == 0 {
				t.Error("expected at least one CreateParagraphBullets request")
			}
		})
	}
}

func TestParseContentLiteralBackslashN(t *testing.T) {
	withLiteral := `- item one\n- item two`
	withNewline := "- item one\n- item two"

	chunksLiteral := parseContent(withLiteral)
	chunksNewline := parseContent(withNewline)

	if len(chunksLiteral) != len(chunksNewline) {
		t.Fatalf("chunk count mismatch: literal %d, newline %d", len(chunksLiteral), len(chunksNewline))
	}
	for i := range chunksLiteral {
		if chunksLiteral[i].Content != chunksNewline[i].Content {
			t.Errorf("chunk %d content: literal %q, newline %q", i, chunksLiteral[i].Content, chunksNewline[i].Content)
		}
	}
}

func TestParseContent(t *testing.T) {
	input := `this is a **bold** word and this is an _italic_ like *this*. This is a list:
- the level of indentation should be 1
  - this content should have a level indentation of 2

and this is back to a level of indentation of zero`

	expected := []Chunk{
		{Content: "this is a ", Style: Style{NormalMask}, IndentationLevel: 0},
		{Content: "bold", Style: Style{BoldMask}, IndentationLevel: 0},
		{Content: " word and this is an ", Style: Style{NormalMask}, IndentationLevel: 0},
		{Content: "italic", Style: Style{ItalicMask}, IndentationLevel: 0},
		{Content: " like ", Style: Style{NormalMask}, IndentationLevel: 0},
		{Content: "this", Style: Style{ItalicMask}, IndentationLevel: 0},
		{Content: ". This is a list:", Style: Style{NormalMask}, IndentationLevel: 0},
		{Content: "the level of indentation should be 1", Style: Style{NormalMask}, IndentationLevel: 1},
		{Content: "this content should have a level indentation of 2", Style: Style{NormalMask}, IndentationLevel: 2},
		{Content: "and this is back to a level of indentation of zero", Style: Style{NormalMask}, IndentationLevel: 0},
	}

	result := parseContent(input)

	if len(result) != len(expected) {
		t.Fatalf("expected %d chunks, got %d", len(expected), len(result))
	}

	for i := 0; i < len(result); i++ {
		if expected[i].Content != result[i].Content {
			t.Errorf("chunk %d content: want %q, have %q", i, expected[i].Content, result[i].Content)
		}
		if !reflect.DeepEqual(expected[i].Style, result[i].Style) {
			t.Errorf("chunk %d style: want %v, have %v", i, expected[i].Style, result[i].Style)
		}
		if expected[i].IndentationLevel != result[i].IndentationLevel {
			t.Errorf("chunk %d indentation: want %d, have %d", i, expected[i].IndentationLevel, result[i].IndentationLevel)
		}
	}

	// Verify paragraph boundaries: chunks in the same paragraph share the same value,
	// chunks in different paragraphs differ.
	sameParagraph := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}}
	for _, pair := range sameParagraph {
		if result[pair[0]].Paragraph != result[pair[1]].Paragraph {
			t.Errorf("chunks %d and %d should share paragraph, got %d and %d",
				pair[0], pair[1], result[pair[0]].Paragraph, result[pair[1]].Paragraph)
		}
	}
	differentParagraph := [][2]int{{6, 7}, {7, 8}, {8, 9}}
	for _, pair := range differentParagraph {
		if result[pair[0]].Paragraph == result[pair[1]].Paragraph {
			t.Errorf("chunks %d and %d should be in different paragraphs, both got %d",
				pair[0], pair[1], result[pair[0]].Paragraph)
		}
	}
}

func TestParseContentInlineCode(t *testing.T) {
	input := "this is `code` here"

	result := parseContent(input)

	expected := []Chunk{
		{Content: "this is ", Style: Style{NormalMask}},
		{Content: "code", Style: Style{CodeMask}},
		{Content: " here", Style: Style{NormalMask}},
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d chunks, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i].Content != expected[i].Content {
			t.Errorf("chunk %d content: want %q, have %q", i, expected[i].Content, result[i].Content)
		}
		if !reflect.DeepEqual(result[i].Style, expected[i].Style) {
			t.Errorf("chunk %d style: want %v, have %v", i, expected[i].Style, result[i].Style)
		}
	}
}

func TestParseContentBoldCode(t *testing.T) {
	input := "text **`bold code`** end"

	result := parseContent(input)

	expected := []Chunk{
		{Content: "text ", Style: Style{NormalMask}},
		{Content: "bold code", Style: Style{BoldMask | CodeMask}},
		{Content: " end", Style: Style{NormalMask}},
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d chunks, got %d", len(expected), len(result))
	}
	for i := range expected {
		if result[i].Content != expected[i].Content {
			t.Errorf("chunk %d content: want %q, have %q", i, expected[i].Content, result[i].Content)
		}
		if !reflect.DeepEqual(result[i].Style, expected[i].Style) {
			t.Errorf("chunk %d style: want %v, have %v", i, expected[i].Style, result[i].Style)
		}
	}
}

func TestInsertMarkdownContentCodeFont(t *testing.T) {
	reqs := InsertMarkdownContent("use `monospace` here", "obj1")

	var foundFont bool
	for _, r := range reqs {
		if r.UpdateTextStyle == nil {
			continue
		}
		if r.UpdateTextStyle.Style.FontFamily == "Courier New" {
			foundFont = true
		}
	}
	if !foundFont {
		t.Error("expected an UpdateTextStyle request with FontFamily 'Courier New'")
	}
}
