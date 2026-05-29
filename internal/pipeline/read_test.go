package pipeline

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/api/slides/v1"
)

type mockSlidesAPI struct {
	presentation *slides.Presentation
	getErr       error
	batchResp    *slides.BatchUpdatePresentationResponse
	batchErr     error
	thumbnail    *slides.Thumbnail
	thumbErr     error
}

func (m *mockSlidesAPI) GetPresentation(string) (*slides.Presentation, error) {
	return m.presentation, m.getErr
}
func (m *mockSlidesAPI) BatchUpdate(string, *slides.BatchUpdatePresentationRequest) (*slides.BatchUpdatePresentationResponse, error) {
	return m.batchResp, m.batchErr
}
func (m *mockSlidesAPI) GetPageThumbnail(string, string) (*slides.Thumbnail, error) {
	return m.thumbnail, m.thumbErr
}

func TestReadPresentation_Basic(t *testing.T) {
	mock := &mockSlidesAPI{
		presentation: &slides.Presentation{
			Slides: []*slides.Page{
				{
					ObjectId: "page1",
					PageElements: []*slides.PageElement{
						{
							ObjectId: "el1",
							Shape: &slides.Shape{
								ShapeType: "TEXT_BOX",
								Text: &slides.TextContent{
									TextElements: []*slides.TextElement{
										{TextRun: &slides.TextRun{Content: "Hello"}},
										{TextRun: &slides.TextRun{Content: " World"}},
									},
								},
							},
						},
					},
				},
				{
					ObjectId:     "page2",
					PageElements: []*slides.PageElement{},
				},
			},
		},
	}

	infos, err := ReadPresentation(context.Background(), mock, "test-pres")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d slides, want 2", len(infos))
	}

	if infos[0].PageObjectID != "page1" {
		t.Errorf("slide 0 PageObjectID = %q, want %q", infos[0].PageObjectID, "page1")
	}
	if infos[0].Index != 0 {
		t.Errorf("slide 0 Index = %d, want 0", infos[0].Index)
	}
	if len(infos[0].TextElements) != 1 {
		t.Fatalf("slide 0 has %d text elements, want 1", len(infos[0].TextElements))
	}
	if infos[0].TextElements[0].Content != "Hello World" {
		t.Errorf("text content = %q, want %q", infos[0].TextElements[0].Content, "Hello World")
	}
	if infos[0].TextElements[0].ObjectID != "el1" {
		t.Errorf("ObjectID = %q, want %q", infos[0].TextElements[0].ObjectID, "el1")
	}

	if len(infos[1].TextElements) != 0 {
		t.Errorf("slide 1 should have no text elements, got %d", len(infos[1].TextElements))
	}
}

func TestReadPresentation_Table(t *testing.T) {
	mock := &mockSlidesAPI{
		presentation: &slides.Presentation{
			Slides: []*slides.Page{
				{
					ObjectId: "page1",
					PageElements: []*slides.PageElement{
						{
							ObjectId: "table1",
							Table: &slides.Table{
								Columns: 2,
								TableRows: []*slides.TableRow{
									{
										TableCells: []*slides.TableCell{
											{Text: &slides.TextContent{TextElements: []*slides.TextElement{{TextRun: &slides.TextRun{Content: "A1"}}}}},
											{Text: &slides.TextContent{TextElements: []*slides.TextElement{{TextRun: &slides.TextRun{Content: "B1"}}}}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	infos, err := ReadPresentation(context.Background(), mock, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos[0].TextElements) != 2 {
		t.Fatalf("expected 2 table cell text elements, got %d", len(infos[0].TextElements))
	}
	if infos[0].TextElements[0].ShapeType != "TABLE" {
		t.Errorf("ShapeType = %q, want TABLE", infos[0].TextElements[0].ShapeType)
	}
	if infos[0].TextElements[0].CellLocation == nil {
		t.Fatal("expected CellLocation for table cell")
	}
	if infos[0].TextElements[0].CellLocation.RowIndex != 0 || infos[0].TextElements[0].CellLocation.ColumnIndex != 0 {
		t.Error("unexpected cell location for first cell")
	}
}

func TestReadPresentation_GroupedElements(t *testing.T) {
	mock := &mockSlidesAPI{
		presentation: &slides.Presentation{
			Slides: []*slides.Page{
				{
					ObjectId: "page1",
					PageElements: []*slides.PageElement{
						{
							ObjectId: "group1",
							ElementGroup: &slides.Group{
								Children: []*slides.PageElement{
									{
										ObjectId: "child1",
										Shape: &slides.Shape{
											ShapeType: "TEXT_BOX",
											Text: &slides.TextContent{
												TextElements: []*slides.TextElement{
													{TextRun: &slides.TextRun{Content: "Grouped text"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	infos, err := ReadPresentation(context.Background(), mock, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos[0].TextElements) != 1 {
		t.Fatalf("expected 1 text element from group, got %d", len(infos[0].TextElements))
	}
	if infos[0].TextElements[0].Content != "Grouped text" {
		t.Errorf("content = %q, want %q", infos[0].TextElements[0].Content, "Grouped text")
	}
}

func TestReadPresentation_Error(t *testing.T) {
	mock := &mockSlidesAPI{getErr: fmt.Errorf("API down")}
	_, err := ReadPresentation(context.Background(), mock, "test")
	if err == nil {
		t.Fatal("expected error")
	}
}
