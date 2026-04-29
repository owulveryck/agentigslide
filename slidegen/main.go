package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"example.com/internal/auth"
	"example.com/internal/fixfonts"
	"example.com/internal/model"
	"example.com/internal/plan"
	islides "example.com/internal/slides"
	"example.com/internal/vertex"
	"example.com/markdown"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/slides/v1"
)

func main() {
	filePath := flag.String("file", "", "Path to markdown file with the presentation request")
	credentials := flag.String("credentials", "", "Path to OAuth2 client credentials JSON (default: GOOGLE_APPLICATION_CREDENTIALS)")
	flag.Parse()

	if *filePath == "" {
		log.Fatal("Usage: slidegen --file <request.md> [--credentials <creds.json>]")
	}

	userRequest, err := os.ReadFile(*filePath)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	templateID := os.Getenv("SLIDES_PREFORMATES_ID")
	if templateID == "" {
		log.Fatal("SLIDES_PREFORMATES_ID environment variable must be set")
	}

	index, err := plan.LoadTemplateIndex("template_index.json")
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	ctx := context.Background()

	// --- Phase 1: Generate plan via Claude (Vertex AI) ---

	log.Println("Generating slide plan via Claude...")
	vc, err := vertex.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	compactIndex := plan.BuildCompactIndex(index)

	genPlan, err := parseUserRequest(ctx, vc, string(userRequest), compactIndex)
	if err != nil {
		log.Fatalf("Failed to generate plan: %v", err)
	}

	presPlan := plan.EnrichPlan(genPlan, index, templateID, string(userRequest))
	log.Printf("Plan generated: %q with %d slide(s)", presPlan.PresentationTitle, len(presPlan.Slides))

	if len(presPlan.Slides) == 0 {
		log.Fatal("Plan has no slides")
	}

	// --- Phase 2: Create presentation via Google Slides/Drive APIs ---

	credFile := *credentials
	if credFile == "" {
		credFile = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	}
	if credFile == "" {
		log.Fatal("Provide --credentials <file> or set GOOGLE_APPLICATION_CREDENTIALS")
	}

	slidesClient, err := auth.GetOAuthClient(ctx, credFile)
	if err != nil {
		log.Fatalf("Failed to get authenticated client: %v", err)
	}

	slidesSrv, err := slides.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Slides service: %v", err)
	}

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(slidesClient))
	if err != nil {
		log.Fatalf("Failed to create Drive service: %v", err)
	}

	presId, err := executePlan(ctx, presPlan, slidesSrv, driveSrv)
	if err != nil {
		log.Fatalf("Failed to execute plan: %v", err)
	}

	url := fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId)

	log.Println("Running fixfonts on generated presentation...")
	if err := fixfonts.Run(ctx, slidesSrv, driveSrv, vc, presId); err != nil {
		log.Printf("Warning: fixfonts failed: %v", err)
	}

	fmt.Println(url)
}

// --- Plan generation (Claude via Vertex AI) ---

func parseUserRequest(ctx context.Context, vc *vertex.Client, userRequest, templateIndexJSON string) (*model.GenerationPlan, error) {
	prompt := fmt.Sprintf(`Tu es un expert en création de présentations professionnelles à partir du template OCTO.

RÈGLES FONDAMENTALES :
1. N'INVENTE AUCUNE INFORMATION. Tout le contenu texte doit provenir exclusivement de la demande utilisateur. Si une information n'est pas dans la demande, ne la fabrique pas.
2. ADÉQUATION STRUCTURE/CONTENU : Le choix de chaque slide est dicté par le nombre d'informations à afficher. Compte les éléments de contenu disponibles dans la demande (bullet points, paragraphes, chiffres clés) et choisis une slide dont le nombre de zones éditables correspond. Par exemple : 3 points à afficher → slide avec 3 zones de contenu, PAS une slide avec 6 zones. Ne duplique JAMAIS du contenu pour remplir des zones vides. Préfère une slide plus simple plutôt qu'une slide trop riche avec des champs laissés vides ou répétés.
2bis. ADÉQUATION TAILLE/CONTENU : Chaque champ éditable indique sa capacité approximative en caractères (~N car.). Place les textes longs dans les grands champs et les textes courts dans les petits champs. Ne mets JAMAIS un texte de plus de N caractères dans un champ indiqué ~N car. Si le texte est trop long pour le champ disponible, résume-le ou choisis une slide avec des champs plus grands.
3. ANTI-DUPLICATION : Chaque texte de la demande ne doit apparaître qu'UNE SEULE FOIS dans toute la présentation. Ne mets JAMAIS le même texte (même reformulé) dans deux champs différents d'une même slide. Si une slide a plus de zones de contenu que de contenus disponibles, choisis une slide plus simple avec moins de zones. Le nombre entre crochets [N champs de contenu] t'aide à comparer avec le nombre d'éléments à placer.
4. La présentation doit être cohérente et compréhensible : les slides intercalaires (titres de section, séparateurs) doivent être placées entre les parties qu'elles introduisent.
5. L'ordre des slides dans le JSON = l'ordre final dans la présentation.
6. EXHAUSTIVITÉ : Chaque section et sous-section de la demande utilisateur doit avoir au moins une slide dédiée. Ne saute aucune partie du contenu fourni. Si la demande contient 4 étapes, génère 4 slides de contenu (pas 2 ou 3).

STRUCTURE ATTENDUE :
- Slide de titre (couverture)
- Pour chaque grande partie : une slide intercalaire de section, puis les slides de contenu
- Slide de conclusion / remerciement / contacts si pertinent

SLIDES DISPONIBLES DANS LE TEMPLATE :
%s

DEMANDE UTILISATEUR :
"""
%s
"""

CONSIGNES POUR LE CONTENU :
- Remplis CHAQUE champ éditable de chaque slide choisie
- Utilise UNIQUEMENT le texte et les informations fournis dans la demande utilisateur
- Pour les champs de type "année" ou "copyright" : utilise 2026
- Pour les numéros de page : ne les inclus pas dans les modifications
- Si la demande ne fournit pas assez de contenu pour remplir un champ, utilise un texte court et neutre en rapport avec le titre de la section (ex: le titre de la partie, ou un tiret)
- Ne génère PAS de bullet points, chiffres ou affirmations qui ne sont pas dans la demande
- RESPECT DES TAILLES : Pour chaque champ, la mention "~N car." indique le nombre maximum approximatif de caractères. Adapte la longueur du texte en conséquence. Un titre dans un champ "petit ~30 car." doit faire moins de 30 caractères. Un texte dans un champ "grand ~300 car." peut être un paragraphe complet.

FORMATAGE MARKDOWN (dans les champs newText) :
- Tu peux utiliser **gras** pour mettre en valeur des mots importants
- Tu peux utiliser *italique* pour des nuances ou termes techniques
- Tu peux utiliser des listes à puces avec - pour structurer le contenu :
  - un seul niveau d'indentation : - item
  - deux niveaux d'indentation :   - sous-item (2 espaces avant le tiret)
- N'utilise PAS d'autres balises markdown (titres #, liens, images, code, etc.)
- Le markdown est optionnel : utilise-le uniquement quand cela améliore la lisibilité

Réponds UNIQUEMENT avec un JSON (pas de texte avant ou après) :
{
  "presentationTitle": "Titre de la présentation",
  "slides": [
    {
      "sourceSlide": 1,
      "modifications": [
        {
          "variableName": "titlemainShape",
          "newText": "Nouveau titre"
        }
      ]
    }
  ]
}

RAPPELS :
- "variableName" doit correspondre exactement à un editableFields.variableName du template
- Tu peux réutiliser la même slide template plusieurs fois avec des contenus différents
- L'ordre des slides est crucial : intercalaire AVANT le contenu de la section
`, templateIndexJSON, userRequest)

	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	responseText, err := vc.RawPredict(ctx, "claude-opus-4-6", messages)
	if err != nil {
		return nil, fmt.Errorf("claude API call failed: %w", err)
	}

	var plan model.GenerationPlan
	if err := json.Unmarshal([]byte(responseText), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w\nResponse was: %s", err, responseText)
	}

	return &plan, nil
}

// --- Plan execution (Google Slides/Drive APIs) ---

func executePlan(ctx context.Context, plan *model.PresentationPlan, slidesSrv *slides.Service, driveSrv *drive.Service) (presId string, err error) {
	log.Printf("Copying template %s...", plan.TemplateID)
	copiedFile, err := driveSrv.Files.Copy(plan.TemplateID, &drive.File{
		Name:    plan.PresentationTitle,
		Parents: []string{"root"},
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to copy template: %w", err)
	}
	presId = copiedFile.Id
	log.Printf("Created presentation: %s", presId)

	pres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get presentation: %w", err)
	}

	pageMap := make(map[string]*slides.Page, len(pres.Slides))
	for _, page := range pres.Slides {
		pageMap[page.ObjectId] = page
	}

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

		log.Printf("Duplicating slide %d/%d (source: %d)...", dupCounter, len(plan.Slides), spec.SourceSlideNumber)
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

	// Reorder slides to match plan order.
	// DuplicateObject places copies next to sources, not at the end.
	// Moving each slide to position 0 in reverse plan order produces the correct order.
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

	freshPres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to re-read presentation: %w", err)
	}
	textPresence := islides.BuildTextPresenceMap(freshPres)

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

			if obj.CellLocation != nil {
				cellLoc := &slides.TableCellLocation{
					RowIndex:    int64(obj.CellLocation.RowIndex),
					ColumnIndex: int64(obj.CellLocation.ColumnIndex),
				}
				cellKey := fmt.Sprintf("%s_%d_%d", actualId, obj.CellLocation.RowIndex, obj.CellLocation.ColumnIndex)
				if textPresence[cellKey] {
					updateRequests = append(updateRequests, &slides.Request{
						DeleteText: &slides.DeleteTextRequest{
							ObjectId:     actualId,
							CellLocation: cellLoc,
							TextRange: &slides.Range{
								Type: "ALL",
							},
						},
					})
				}
				updateRequests = append(updateRequests, markdown.InsertMarkdownContentInCell(*obj.NewValue, actualId, cellLoc)...)
			} else {
				if textPresence[actualId] {
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

	log.Printf("Presentation created successfully: https://docs.google.com/presentation/d/%s/edit", presId)
	return presId, nil
}
