package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"example.com/markdown"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

// --- Input structs (from generateSlideList output) ---

type PresentationPlan struct {
	PresentationTitle string      `json:"presentationTitle"`
	TemplateID        string      `json:"templateId"`
	GeneratedAt       string      `json:"generatedAt"`
	SourceRequest     string      `json:"sourceRequest"`
	Slides            []SlideSpec `json:"slides"`
}

type SlideSpec struct {
	Position          int              `json:"position"`
	SourceSlideNumber int              `json:"sourceSlideNumber"`
	SourceSlideID     string           `json:"sourceSlideId"`
	Intention         string           `json:"intention"`
	Description       string           `json:"description"`
	PreviewImage      string           `json:"previewImage"`
	EditableObjects   []EditableObject `json:"editableObjects"`
}

type EditableObject struct {
	ObjectID     string  `json:"objectId"`
	VariableName string  `json:"variableName"`
	Role         string  `json:"role"`
	ElementType  string  `json:"elementType"`
	Placeholder  *string `json:"placeholder"`
	Description  string  `json:"description"`
	Location     string  `json:"location"`
	CurrentValue string  `json:"currentValue"`
	NewValue     *string `json:"newValue,omitempty"`
	Modified     bool    `json:"modified"`
}

// --- slideRef tracks objectId mapping for each planned slide ---

type slideRef struct {
	pageObjectId string
	elementMap   map[string]string
}

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

	var plan PresentationPlan
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

	client, err := getOAuthClient(ctx, credFile)
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

// --- Authentication ---

func getOAuthClient(ctx context.Context, credentialsFile string) (*http.Client, error) {
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file: %w", err)
	}

	scopes := []string{drive.DriveScope, slides.PresentationsScope}

	// Try as OAuth2 client credentials (type "installed" / "web")
	config, err := google.ConfigFromJSON(b, scopes...)
	if err == nil {
		tokenFile := tokenCachePath()
		tok, err := tokenFromFile(tokenFile)
		if err != nil {
			tok, err = getTokenFromWeb(config)
			if err != nil {
				return nil, err
			}
			if err := saveToken(tokenFile, tok); err != nil {
				log.Printf("Warning: failed to save token: %v", err)
			}
		}
		return config.Client(ctx, tok), nil
	}

	// Fall back to default credentials (authorized_user, service_account)
	creds, err := google.CredentialsFromJSON(ctx, b, scopes...)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %w", err)
	}
	return oauth2.NewClient(ctx, creds.TokenSource), nil
}

func tokenCachePath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".credentials")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "slideappscripter-token.json")
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Go to the following link in your browser then type the authorization code:\n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return nil, fmt.Errorf("unable to read authorization code: %w", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %w", err)
	}
	return tok, nil
}

func saveToken(path string, token *oauth2.Token) error {
	fmt.Fprintf(os.Stderr, "Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(token)
}

// --- Plan execution ---

func executePlan(ctx context.Context, plan *PresentationPlan, slidesSrv *slides.Service, driveSrv *drive.Service) (string, error) {
	// Step 1: Copy template via Drive API
	log.Printf("Copying template %s...", plan.TemplateID)
	copiedFile, err := driveSrv.Files.Copy(plan.TemplateID, &drive.File{
		Name: plan.PresentationTitle,
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
	refs := make([]slideRef, 0, len(plan.Slides))
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

		for _, elId := range collectElementIds(srcPage) {
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

		refs = append(refs, slideRef{pageObjectId: newPageId, elementMap: objectIds})
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
				SlideObjectIds:  []string{refs[i].pageObjectId},
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
			actualId := ref.elementMap[obj.ObjectID]
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

func collectElementIds(page *slides.Page) []string {
	var ids []string
	for _, el := range page.PageElements {
		ids = append(ids, collectPageElementIds(el)...)
	}
	return ids
}

func collectPageElementIds(el *slides.PageElement) []string {
	ids := []string{el.ObjectId}
	if el.ElementGroup != nil {
		for _, child := range el.ElementGroup.Children {
			ids = append(ids, collectPageElementIds(child)...)
		}
	}
	return ids
}
