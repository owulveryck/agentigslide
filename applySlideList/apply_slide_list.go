package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"example.com/internal/auth"
	"example.com/internal/model"
	islides "example.com/internal/slides"
	"example.com/markdown"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	planPath := flag.String("plan", "", "Path to presentation plan JSON (use - for stdin)")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (default: GOOGLE_APPLICATION_CREDENTIALS)")
	flag.Parse()

	if *planPath == "" {
		log.Fatal("Usage: apply_slide_list --plan <file.json> or --plan -")
	}

	var planData []byte
	var err error
	if *planPath == "-" {
		planData, err = io.ReadAll(os.Stdin)
	} else {
		planData, err = os.ReadFile(*planPath)
	}
	if err != nil {
		log.Fatalf("Failed to read plan: %v", err)
	}

	var plan model.PresentationPlan
	if err := json.Unmarshal(planData, &plan); err != nil {
		log.Fatalf("Failed to parse plan: %v", err)
	}

	if len(plan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	ctx := context.Background()

	credFile := *credentials
	if credFile == "" {
		credFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set GOOGLE_APPLICATION_CREDENTIALS")
	}

	client, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	url, err := executePlan(ctx, &plan, slidesSrv, driveSrv)
	if err != nil {
		log.Fatalf("Failed to execute plan: %v", err)
	}

	fmt.Println(url)
}

// --- Plan execution ---

func executePlan(ctx context.Context, plan *model.PresentationPlan, slidesSrv *slides.Service, driveSrv *drive.Service) (string, error) {
	// Step 1: Copy template via Drive API
	log.Printf("Copying template %s...", plan.TemplateID)
	copiedFile, err := driveSrv.Files.Copy(plan.TemplateID, &drive.File{
		Name:    plan.PresentationTitle,
		Parents: []string{"root"},
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to copy template: %w", err)
	}
	presId := copiedFile.Id
	log.Printf("Created presentation: %s", presId)

	// Step 2: Get presentation structure
	pres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get presentation: %w", err)
	}

	pageMap := make(map[string]*slides.Page, len(pres.Slides))
	for _, page := range pres.Slides {
		pageMap[page.ObjectId] = page
	}

	// Step 3: Duplicate each plan slide (creates copies at the end of the presentation)
	// Every slide in the plan gets its own fresh copy — this handles duplicates naturally
	// and the order of copies matches the plan order.
	refs := make([]model.SlideRef, 0, len(plan.Slides))
	dupCounter := 0

	for _, spec := range plan.Slides {
		srcId := spec.SourceSlideID
		srcPage, ok := pageMap[srcId]
		if !ok {
			log.Printf("Warning: slide %d (id=%s) not found in presentation, skipping", spec.SourceSlideNumber, srcId)
			continue
		}

		dupCounter++
		objectIds := make(map[string]string)
		newPageId := fmt.Sprintf("d%d_%s", dupCounter, srcId)
		objectIds[srcId] = newPageId

		for _, elId := range islides.CollectElementIds(srcPage) {
			objectIds[elId] = fmt.Sprintf("d%d_%s", dupCounter, elId)
		}

		log.Printf("Duplicating slide %d (source: %d)...", dupCounter, spec.SourceSlideNumber)
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: []*slides.Request{{
				DuplicateObject: &slides.DuplicateObjectRequest{
					ObjectId:  srcId,
					ObjectIds: objectIds,
				},
			}},
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to duplicate slide %d: %w", spec.SourceSlideNumber, err)
		}

		refs = append(refs, model.SlideRef{PageObjectID: newPageId, ElementMap: objectIds})
	}

	// Step 4: Delete all original template pages (keep only our copies at the end)
	var deleteRequests []*slides.Request
	for _, page := range pres.Slides {
		deleteRequests = append(deleteRequests, &slides.Request{
			DeleteObject: &slides.DeleteObjectRequest{
				ObjectId: page.ObjectId,
			},
		})
	}

	if len(deleteRequests) > 0 {
		log.Printf("Deleting %d original template slide(s)...", len(deleteRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: deleteRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to delete original slides: %w", err)
		}
	}

	var reorderRequests []*slides.Request
	for i := len(refs) - 1; i >= 0; i-- {
		reorderRequests = append(reorderRequests, &slides.Request{
			UpdateSlidesPosition: &slides.UpdateSlidesPositionRequest{
				SlideObjectIds:  []string{refs[i].PageObjectID},
				InsertionIndex:  0,
				ForceSendFields: []string{"InsertionIndex"},
			},
		})
	}

	if len(reorderRequests) > 0 {
		log.Printf("Reordering %d slide(s)...", len(refs))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: reorderRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to reorder slides: %w", err)
		}
	}

	// Update text content with markdown formatting
	var updateRequests []*slides.Request
	for i, spec := range plan.Slides {
		if i >= len(refs) {
			break
		}
		ref := refs[i]
		for _, obj := range spec.EditableObjects {
			if !obj.Modified || obj.NewValue == nil || obj.ObjectID == "" {
				continue
			}
			actualId := ref.ElementMap[obj.ObjectID]
			if actualId == "" {
				actualId = obj.ObjectID
			}

			if obj.CurrentValue != "" {
				updateRequests = append(updateRequests, &slides.Request{
					DeleteText: &slides.DeleteTextRequest{
						ObjectId: actualId,
						TextRange: &slides.Range{
							Type: "ALL",
						},
					},
				})
			}
			updateRequests = append(updateRequests, markdown.InsertMarkdownContent(*obj.NewValue, actualId)...)
		}
	}
	markdown.SortRequests(updateRequests)

	if len(updateRequests) > 0 {
		log.Printf("Updating text in %d element(s)...", len(updateRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to update text content: %w", err)
		}
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)
	log.Printf("Presentation created successfully: %s", url)
	return url, nil
}
