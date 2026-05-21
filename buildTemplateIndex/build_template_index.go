// Command buildTemplateIndex aggregates analysis.json files from all analyzed
// slides into a single template_index.json. For each slide, it extracts
// keywords, generates semantic variable names for editable fields, computes
// field dimensions and character capacity, and resolves table cell mappings.
//
// Usage:
//
//	go run buildTemplateIndex/build_template_index.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/owulveryck/agentigslide/internal/config"
	"github.com/owulveryck/agentigslide/internal/model"
	tidx "github.com/owulveryck/agentigslide/internal/templateindex"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: build_template_index\n\nAggregates analysis.json files into template_index.json.\n")
		config.PrintAllUsage(
			struct {
				Prefix string
				Spec   any
			}{"SLIDES", &config.SlidesConfig{}},
		)
	}
	flag.Parse()

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	baseDir := slidesCfg.TemplateDir()

	var analyses []model.SlideAnalysis
	err = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "analysis.json" {
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("Warning: failed to read %s: %v", path, err)
				return nil
			}

			var analysis model.SlideAnalysis
			if err := json.Unmarshal(data, &analysis); err != nil {
				log.Printf("Warning: failed to parse %s: %v", path, err)
				return nil
			}

			analyses = append(analyses, analysis)
		}
		return nil
	})

	if err != nil {
		log.Fatalf("Failed to walk template directory: %v", err)
	}

	if len(analyses) == 0 {
		log.Fatal("No analysis.json files found")
	}

	sort.Slice(analyses, func(i, j int) bool {
		return analyses[i].SlideNumber < analyses[j].SlideNumber
	})

	index := model.TemplateIndex{
		TemplateID: slidesCfg.TemplateID,
		Slides:     make([]model.TemplateSlide, 0, len(analyses)),
	}

	for _, analysis := range analyses {
		slide := model.TemplateSlide{
			SlideNumber:    analysis.SlideNumber,
			SlideID:        analysis.SlideID,
			Intention:      analysis.Intention,
			Description:    analysis.Description,
			Category:       analysis.Category,
			UseCaseTags:    analysis.UseCaseTags,
			VisualStyle:    analysis.VisualStyle,
			Keywords:       tidx.ExtractKeywords(analysis),
			EditableFields: make([]model.EditableFieldSummary, 0, len(analysis.EditableElements)),
			VisualElements: make([]model.VisualElementSummary, 0),
		}

		slideContent, err := loadSlideContent(baseDir, analysis.SlideNumber)
		if err != nil {
			log.Printf("Warning: failed to load content.json for slide %d: %v", analysis.SlideNumber, err)
			slideContent = nil
		}

		var rawTextMap map[string]string
		if slideContent != nil {
			rawTextMap = tidx.ExtractShapeTextMap(slideContent)
		}

		for _, elem := range analysis.EditableElements {
			role := tidx.InferRole(elem)

			varName := ""
			if elem.VariableName != "" {
				clean := strings.TrimSuffix(elem.VariableName, "Shape")
				varName = tidx.ToCamelCase(clean) + "Shape"
			} else if slideContent != nil {
				varName = tidx.GenerateVariableName(elem, slideContent, &analysis)
			}

			content := elem.Content
			if tidx.IsPlaceholderContent(content) {
				content = ""
			}

			rawContent := ""
			if rawTextMap != nil {
				rawContent = rawTextMap[elem.ObjectID]
			}

			var widthPt, heightPt float64
			var maxChars int
			if slideContent != nil {
				if pageElem := tidx.FindPageElementByID(slideContent, elem.ObjectID); pageElem != nil {
					widthPt, heightPt = tidx.ComputeElementSize(pageElem)
					font := tidx.ExtractPredominantFont(pageElem)
					maxChars = tidx.EstimateMaxChars(widthPt, heightPt, font)
				}
			}

			field := model.EditableFieldSummary{
				ObjectID:     elem.ObjectID,
				Role:         role,
				Placeholder:  elem.Placeholder,
				Content:      content,
				RawContent:   rawContent,
				VariableName: varName,
				WidthPt:      widthPt,
				HeightPt:     heightPt,
				MaxChars:     maxChars,
			}

			slide.EditableFields = append(slide.EditableFields, field)
		}

		if slideContent != nil {
			tidx.ResolveTableCells(slide.EditableFields, slideContent)
		}
		tidx.DeduplicateVariableNames(slide.EditableFields)

		for _, elem := range analysis.VisualElements {
			if elem.ObjectID != nil && *elem.ObjectID != "" {
				slide.VisualElements = append(slide.VisualElements, model.VisualElementSummary(elem))
			}
		}

		slide.LayoutDescription = tidx.GenerateLayoutDescription(analysis, slideContent, slide.EditableFields)

		index.Slides = append(index.Slides, slide)
	}

	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal index: %v", err)
	}

	outputPath := slidesCfg.EffectiveTemplateIndex()
	if err := os.WriteFile(outputPath, indexJSON, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", outputPath, err)
	}

	fmt.Printf("Template index generated successfully!\n")
	fmt.Printf("- Template ID: %s\n", slidesCfg.TemplateID)
	fmt.Printf("- Slides indexed: %d\n", len(index.Slides))
	fmt.Printf("- Output: %s\n", outputPath)
}

func loadSlideContent(baseDir string, slideNumber int) (*model.SlideContent, error) {
	contentPath := filepath.Join(baseDir, fmt.Sprintf("%d", slideNumber), "content.json")
	data, err := os.ReadFile(contentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read content.json: %w", err)
	}

	var content model.SlideContent
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("failed to parse content.json: %w", err)
	}

	return &content, nil
}
