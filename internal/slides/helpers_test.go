package slides

import (
	"testing"

	gslides "google.golang.org/api/slides/v1"
)

// --- CollectElementIds tests ---

func TestCollectElementIds_EmptyPage(t *testing.T) {
	page := &gslides.Page{}
	ids := CollectElementIds(page)
	if len(ids) != 0 {
		t.Errorf("expected 0 IDs for empty page, got %d", len(ids))
	}
}

func TestCollectElementIds_NilPageElements(t *testing.T) {
	page := &gslides.Page{
		PageElements: nil,
	}
	ids := CollectElementIds(page)
	if len(ids) != 0 {
		t.Errorf("expected 0 IDs for page with nil elements, got %d", len(ids))
	}
}

func TestCollectElementIds_FlatElements(t *testing.T) {
	page := &gslides.Page{
		PageElements: []*gslides.PageElement{
			{ObjectId: "elem1"},
			{ObjectId: "elem2"},
			{ObjectId: "elem3"},
		},
	}
	ids := CollectElementIds(page)
	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}
	expected := []string{"elem1", "elem2", "elem3"}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestCollectElementIds_NestedGroups(t *testing.T) {
	page := &gslides.Page{
		PageElements: []*gslides.PageElement{
			{ObjectId: "top1"},
			{
				ObjectId: "group1",
				ElementGroup: &gslides.Group{
					Children: []*gslides.PageElement{
						{ObjectId: "child1"},
						{
							ObjectId: "subgroup1",
							ElementGroup: &gslides.Group{
								Children: []*gslides.PageElement{
									{ObjectId: "grandchild1"},
									{ObjectId: "grandchild2"},
								},
							},
						},
					},
				},
			},
			{ObjectId: "top2"},
		},
	}
	ids := CollectElementIds(page)
	expected := []string{"top1", "group1", "child1", "subgroup1", "grandchild1", "grandchild2", "top2"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d IDs, got %d: %v", len(expected), len(ids), ids)
	}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

// --- CollectPageElementIds tests ---

func TestCollectPageElementIds_SimpleElement(t *testing.T) {
	el := &gslides.PageElement{ObjectId: "simple1"}
	ids := CollectPageElementIds(el)
	if len(ids) != 1 {
		t.Fatalf("expected 1 ID, got %d", len(ids))
	}
	if ids[0] != "simple1" {
		t.Errorf("got %q, want %q", ids[0], "simple1")
	}
}

func TestCollectPageElementIds_ElementWithGroup(t *testing.T) {
	el := &gslides.PageElement{
		ObjectId: "group1",
		ElementGroup: &gslides.Group{
			Children: []*gslides.PageElement{
				{ObjectId: "childA"},
				{ObjectId: "childB"},
			},
		},
	}
	ids := CollectPageElementIds(el)
	expected := []string{"group1", "childA", "childB"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d IDs, got %d", len(expected), len(ids))
	}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestCollectPageElementIds_DeeplyNested(t *testing.T) {
	el := &gslides.PageElement{
		ObjectId: "level0",
		ElementGroup: &gslides.Group{
			Children: []*gslides.PageElement{
				{
					ObjectId: "level1",
					ElementGroup: &gslides.Group{
						Children: []*gslides.PageElement{
							{
								ObjectId: "level2",
								ElementGroup: &gslides.Group{
									Children: []*gslides.PageElement{
										{ObjectId: "level3"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	ids := CollectPageElementIds(el)
	expected := []string{"level0", "level1", "level2", "level3"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d IDs, got %d: %v", len(expected), len(ids), ids)
	}
	for i, want := range expected {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

// --- BuildTextPresenceMap tests ---

func TestBuildTextPresenceMap_EmptyPresentation(t *testing.T) {
	pres := &gslides.Presentation{}
	m := BuildTextPresenceMap(pres)
	if len(m) != 0 {
		t.Errorf("expected empty map for empty presentation, got %d entries", len(m))
	}
}

func TestBuildTextPresenceMap_NilSlides(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: nil,
	}
	m := BuildTextPresenceMap(pres)
	if len(m) != 0 {
		t.Errorf("expected empty map for nil slides, got %d entries", len(m))
	}
}

func TestBuildTextPresenceMap_ShapesWithText(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "shape1",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{
										TextRun: &gslides.TextRun{
											Content: "Hello World",
										},
									},
								},
							},
						},
					},
					{
						ObjectId: "shape2",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{
										TextRun: &gslides.TextRun{
											Content: "Some text",
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
	m := BuildTextPresenceMap(pres)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if !m["shape1"] {
		t.Error("expected shape1 to be present in map")
	}
	if !m["shape2"] {
		t.Error("expected shape2 to be present in map")
	}
}

func TestBuildTextPresenceMap_ShapesWithEmptyText(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "emptyShape",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{
										TextRun: &gslides.TextRun{
											Content: "\n",
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
	m := BuildTextPresenceMap(pres)
	if len(m) != 0 {
		t.Errorf("expected empty map for shape with only newline content, got %d entries", len(m))
	}
}

func TestBuildTextPresenceMap_ShapeWithNilText(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "noTextShape",
						Shape: &gslides.Shape{
							Text: nil,
						},
					},
				},
			},
		},
	}
	m := BuildTextPresenceMap(pres)
	if len(m) != 0 {
		t.Errorf("expected empty map for shape with nil text, got %d entries", len(m))
	}
}

func TestBuildTextPresenceMap_ShapeWithNilShape(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "noShape",
						Shape:    nil,
					},
				},
			},
		},
	}
	m := BuildTextPresenceMap(pres)
	if len(m) != 0 {
		t.Errorf("expected empty map for element with nil shape, got %d entries", len(m))
	}
}

func TestBuildTextPresenceMap_TablesWithText(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "table1",
						Table: &gslides.Table{
							TableRows: []*gslides.TableRow{
								{
									TableCells: []*gslides.TableCell{
										{
											Text: &gslides.TextContent{
												TextElements: []*gslides.TextElement{
													{
														TextRun: &gslides.TextRun{
															Content: "Cell 0,0",
														},
													},
												},
											},
										},
										{
											Text: &gslides.TextContent{
												TextElements: []*gslides.TextElement{
													{
														TextRun: &gslides.TextRun{
															Content: "Cell 0,1",
														},
													},
												},
											},
										},
									},
								},
								{
									TableCells: []*gslides.TableCell{
										{
											Text: &gslides.TextContent{
												TextElements: []*gslides.TextElement{
													{
														TextRun: &gslides.TextRun{
															Content: "\n",
														},
													},
												},
											},
										},
										{
											Text: &gslides.TextContent{
												TextElements: []*gslides.TextElement{
													{
														TextRun: &gslides.TextRun{
															Content: "Cell 1,1",
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
			},
		},
	}
	m := BuildTextPresenceMap(pres)
	// cell (0,0), (0,1), (1,1) have text; cell (1,0) has only newline
	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(m), m)
	}
	expectedKeys := []string{"table1_0_0", "table1_0_1", "table1_1_1"}
	for _, key := range expectedKeys {
		if !m[key] {
			t.Errorf("expected key %q to be present in map", key)
		}
	}
	if m["table1_1_0"] {
		t.Error("expected key table1_1_0 to NOT be present (only newline content)")
	}
}

func TestBuildTextPresenceMap_TableWithNilCellText(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "table2",
						Table: &gslides.Table{
							TableRows: []*gslides.TableRow{
								{
									TableCells: []*gslides.TableCell{
										{Text: nil},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	m := BuildTextPresenceMap(pres)
	if len(m) != 0 {
		t.Errorf("expected empty map for table with nil cell text, got %d entries", len(m))
	}
}

func TestBuildTextPresenceMap_MixedShapesAndTables(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "shapeA",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{TextRun: &gslides.TextRun{Content: "Shape text"}},
								},
							},
						},
					},
					{
						ObjectId: "tableA",
						Table: &gslides.Table{
							TableRows: []*gslides.TableRow{
								{
									TableCells: []*gslides.TableCell{
										{
											Text: &gslides.TextContent{
												TextElements: []*gslides.TextElement{
													{TextRun: &gslides.TextRun{Content: "Table cell"}},
												},
											},
										},
									},
								},
							},
						},
					},
					{
						ObjectId: "emptyShapeB",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{TextRun: &gslides.TextRun{Content: "\n\n"}},
								},
							},
						},
					},
				},
			},
		},
	}
	m := BuildTextPresenceMap(pres)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(m), m)
	}
	if !m["shapeA"] {
		t.Error("expected shapeA to be present")
	}
	if !m["tableA_0_0"] {
		t.Error("expected tableA_0_0 to be present")
	}
	if m["emptyShapeB"] {
		t.Error("expected emptyShapeB to NOT be present (only newlines)")
	}
}

func TestBuildTextPresenceMap_MultipleSlides(t *testing.T) {
	pres := &gslides.Presentation{
		Slides: []*gslides.Page{
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "slide1_shape",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{TextRun: &gslides.TextRun{Content: "Slide 1"}},
								},
							},
						},
					},
				},
			},
			{
				PageElements: []*gslides.PageElement{
					{
						ObjectId: "slide2_shape",
						Shape: &gslides.Shape{
							Text: &gslides.TextContent{
								TextElements: []*gslides.TextElement{
									{TextRun: &gslides.TextRun{Content: "Slide 2"}},
								},
							},
						},
					},
				},
			},
		},
	}
	m := BuildTextPresenceMap(pres)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries across slides, got %d", len(m))
	}
	if !m["slide1_shape"] {
		t.Error("expected slide1_shape to be present")
	}
	if !m["slide2_shape"] {
		t.Error("expected slide2_shape to be present")
	}
}

// --- HasNonEmptyText tests ---

func TestHasNonEmptyText_NilTextElements(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: nil,
	}
	if HasNonEmptyText(tc) {
		t.Error("expected false for nil TextElements")
	}
}

func TestHasNonEmptyText_EmptyTextElements(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{},
	}
	if HasNonEmptyText(tc) {
		t.Error("expected false for empty TextElements slice")
	}
}

func TestHasNonEmptyText_OnlyNewlines(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: &gslides.TextRun{Content: "\n"}},
			{TextRun: &gslides.TextRun{Content: "\n\n\n"}},
		},
	}
	if HasNonEmptyText(tc) {
		t.Error("expected false for text content with only newlines")
	}
}

func TestHasNonEmptyText_RealText(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: &gslides.TextRun{Content: "Hello"}},
		},
	}
	if !HasNonEmptyText(tc) {
		t.Error("expected true for text content with real text")
	}
}

func TestHasNonEmptyText_TextFollowedByNewline(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: &gslides.TextRun{Content: "Title\n"}},
		},
	}
	if !HasNonEmptyText(tc) {
		t.Error("expected true for text content with text followed by newline")
	}
}

func TestHasNonEmptyText_NilTextRun(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: nil},
		},
	}
	if HasNonEmptyText(tc) {
		t.Error("expected false when TextRun is nil")
	}
}

func TestHasNonEmptyText_MixedEmptyAndNonEmpty(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: &gslides.TextRun{Content: "\n"}},
			{TextRun: nil},
			{TextRun: &gslides.TextRun{Content: "actual content"}},
			{TextRun: &gslides.TextRun{Content: "\n"}},
		},
	}
	if !HasNonEmptyText(tc) {
		t.Error("expected true when at least one TextRun has real content")
	}
}

func TestHasNonEmptyText_AllEmptyMixed(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: nil},
			{TextRun: &gslides.TextRun{Content: "\n"}},
			{TextRun: nil},
			{TextRun: &gslides.TextRun{Content: "\n\n"}},
		},
	}
	if HasNonEmptyText(tc) {
		t.Error("expected false when all TextRuns are nil or only newlines")
	}
}

func TestHasNonEmptyText_EmptyStringContent(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: &gslides.TextRun{Content: ""}},
		},
	}
	if HasNonEmptyText(tc) {
		t.Error("expected false for TextRun with empty string content")
	}
}

func TestHasNonEmptyText_WhitespaceContent(t *testing.T) {
	tc := &gslides.TextContent{
		TextElements: []*gslides.TextElement{
			{TextRun: &gslides.TextRun{Content: "   "}},
		},
	}
	if !HasNonEmptyText(tc) {
		t.Error("expected true for TextRun with whitespace (spaces are non-empty, only trailing newlines are trimmed)")
	}
}
