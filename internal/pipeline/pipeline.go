// Package pipeline provides the shared presentation generation logic used by
// both the slidegen CLI and the MCP server. It handles prompt construction,
// Claude API communication via Vertex AI, and plan execution against the
// Google Slides and Drive APIs.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/owulveryck/slideAppScripter/internal/model"
	islides "github.com/owulveryck/slideAppScripter/internal/slides"
	"github.com/owulveryck/slideAppScripter/internal/vertex"
	"github.com/owulveryck/slideAppScripter/markdown"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/slides/v1"
)

// AmendPromptTemplate is the French prompt template for amending an existing
// slide plan. It includes the existing plan, the slide catalog, and the user's
// amendment request.
const AmendPromptTemplate = `Tu es un expert en création de présentations professionnelles à partir d'un catalogue de slides template.

PLAN EXISTANT :
"""
%s
"""

SLIDES DISPONIBLES :
%s

DEMANDE DE MODIFICATION :
"""
%s
"""

Modifie le plan existant selon la demande de modification. Tu peux :
- Ajouter de nouvelles slides depuis le catalogue
- Supprimer des slides existantes du plan
- Modifier le contenu texte des slides existantes
- Réorganiser l'ordre des slides
- Remplacer une slide par une autre du catalogue

RÈGLES (identiques à la génération initiale) :
1. ADÉQUATION NOMBRE DE ZONES / CONTENU : Choisis des slides dont le nombre de zones [N contenu] correspond au contenu à placer.
2. ADÉQUATION TAILLE : Respecte les capacités ~N caractères max de chaque champ.
3. PAS D'INVENTION : Tout le contenu texte doit provenir de la demande utilisateur originale ou de la demande de modification. Ne fabrique rien.
4. ANTI-DUPLICATION : Chaque texte ne doit apparaître qu'UNE SEULE FOIS dans toute la présentation.
5. COHÉRENCE : Les slides intercalaires doivent précéder les slides de contenu qu'elles introduisent.

Réponds UNIQUEMENT en JSON avec ce format :
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
- "variableName" doit correspondre exactement à un champ du catalogue ci-dessus
- Tu peux réutiliser la même slide template plusieurs fois avec des contenus différents
- L'ordre des slides dans le JSON = l'ordre final dans la présentation
- Formatage markdown autorisé dans newText : **gras**, *italique*, listes avec - (2 espaces pour sous-items)
`

// BuildAmendPrompt constructs the prompt for amending an existing plan. It
// inserts the existing plan summary, template catalog, and amendment request
// into the AmendPromptTemplate.
func BuildAmendPrompt(compactIndex, existingPlanJSON, amendmentRequest, extraInstructions string) string {
	prompt := fmt.Sprintf(AmendPromptTemplate, existingPlanJSON, compactIndex, amendmentRequest)
	if extraInstructions != "" {
		prompt += "\nINSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + extraInstructions + "\n"
	}
	return prompt
}

// PlanToGenerationSummary converts an enriched PresentationPlan back to a
// GenerationPlan JSON string. This is used to show Claude the current state of
// a plan when amending it.
func PlanToGenerationSummary(p *model.PresentationPlan) string {
	summary := model.GenerationPlan{
		PresentationTitle: p.PresentationTitle,
	}
	for _, s := range p.Slides {
		sr := model.SlideRequest{
			SourceSlide: s.SourceSlideNumber,
		}
		for _, obj := range s.EditableObjects {
			if obj.Modified && obj.NewValue != nil {
				sr.Modifications = append(sr.Modifications, model.TextModification{
					VariableName: obj.VariableName,
					NewText:      *obj.NewValue,
				})
			}
		}
		summary.Slides = append(summary.Slides, sr)
	}
	data, _ := json.MarshalIndent(summary, "", "  ")
	return string(data)
}

// DefaultPromptTemplate is the French prompt template sent to Claude for
// generating a slide plan from a user request and the template index.
const DefaultPromptTemplate = `Tu es un expert en création de présentations professionnelles à partir d'un catalogue de slides template.

--- SÉLECTION DES SLIDES ---

1. ADÉQUATION NOMBRE DE ZONES / CONTENU : Compte les éléments de contenu dans la demande (bullet points, paragraphes, chiffres) et choisis une slide dont le nombre entre crochets [N contenu] correspond. Exemple : 3 points → slide [3 contenu], pas [6 contenu]. Si une slide a plus de zones que d'éléments à placer, choisis une slide plus simple. Ne choisis JAMAIS une slide pour la remplir avec du texte inventé ou répété.
2. ADÉQUATION TAILLE : Chaque champ indique ~N caractères max. Place les textes longs dans les grands champs, les courts dans les petits. Ne mets JAMAIS un texte plus long que la capacité indiquée. Si le texte est trop long, résume-le ou choisis une slide avec des champs plus grands.
3. ADÉQUATION DISPOSITION : La ligne "disposition:" décrit la structure visuelle (colonnes, grille). Choisis une slide dont la disposition correspond à la structure de ton contenu. 3 arguments parallèles → slide 3 colonnes, pas 2 colonnes.
4. DIVERSITÉ : Explore l'ENSEMBLE du catalogue pour trouver les slides les plus adaptées. Ne te limite pas aux premières ni aux dernières.

--- CONTENU ---

5. PAS D'INVENTION : Tout le contenu texte doit provenir exclusivement de la demande utilisateur. Si une information n'est pas dans la demande, ne la fabrique pas. Ne génère pas de bullet points, chiffres ou affirmations absents de la demande.
6. EXHAUSTIVITÉ : Chaque section et sous-section de la demande doit avoir au moins une slide dédiée. Ne saute aucune partie. 4 étapes dans la demande → 4 slides de contenu.
7. ANTI-DUPLICATION : Chaque texte ne doit apparaître qu'UNE SEULE FOIS dans toute la présentation. Ne mets jamais le même texte dans deux champs différents, même reformulé.

--- STRUCTURE ---

8. COHÉRENCE : Les slides intercalaires (titres de section) doivent être placées avant les slides de contenu qu'elles introduisent. L'ordre dans le JSON = l'ordre final.

STRUCTURE ATTENDUE :
- Slide de titre (couverture)
- Pour chaque partie : une slide intercalaire, puis les slides de contenu
- Slide de conclusion / remerciement si pertinent

SLIDES DISPONIBLES :
%s

DEMANDE UTILISATEUR :
"""
%s
"""

CONSIGNES POUR LE CONTENU :
- Remplis chaque champ éditable des slides choisies
- Utilise UNIQUEMENT le texte de la demande utilisateur
- Si la demande ne fournit pas de contenu pour un champ, omets-le des modifications ou mets "-"
- Préfère une slide plus simple plutôt que de remplir des zones avec du texte inventé
- RESPECT DES TAILLES : ~N indique le max de caractères. Adapte la longueur en conséquence
- Formatage markdown autorisé dans newText : **gras**, *italique*, listes avec - (2 espaces pour sous-items)

Réponds UNIQUEMENT en JSON :
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
- "variableName" doit correspondre exactement à un champ du catalogue ci-dessus
- Tu peux réutiliser la même slide template plusieurs fois avec des contenus différents
- L'ordre des slides est crucial : intercalaire AVANT le contenu de la section
`

// LoadTemplateInstructions loads additional template-specific instructions from
// PROMPT.md in the given template directory. These are appended to the generic
// DefaultPromptTemplate. Returns an empty string if the file does not exist.
func LoadTemplateInstructions(templateDir string) string {
	data, err := os.ReadFile(filepath.Join(templateDir, "PROMPT.md"))
	if err != nil {
		return ""
	}
	slog.Info("loaded template-specific instructions", "path", filepath.Join(templateDir, "PROMPT.md"))
	return strings.TrimSpace(string(data))
}

// BuildPrompt inserts the template index and user request into the prompt
// template, then appends any template-specific instructions.
func BuildPrompt(promptTemplate, templateIndexJSON, userRequest, extraInstructions string) string {
	prompt := fmt.Sprintf(promptTemplate, templateIndexJSON, userRequest)
	if extraInstructions != "" {
		prompt += "\n\nINSTRUCTIONS SPÉCIFIQUES AU TEMPLATE :\n" + extraInstructions + "\n"
	}
	return prompt
}

// SendPrompt sends a prompt to Claude via Vertex AI and parses the JSON response
// into a GenerationPlan.
func SendPrompt(ctx context.Context, vc *vertex.Client, modelName, prompt string) (*model.GenerationPlan, error) {
	messages := []vertex.Message{{
		Role: "user",
		Content: []vertex.ContentBlock{{
			Type: "text",
			Text: prompt,
		}},
	}}

	responseText, err := vc.RawPredict(ctx, modelName, messages, vertex.WithTemperature(0.2))
	if err != nil {
		return nil, fmt.Errorf("claude API call failed: %w", err)
	}

	var p model.GenerationPlan
	if err := json.Unmarshal([]byte(responseText), &p); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w\nResponse was: %s", err, responseText)
	}

	return &p, nil
}

// ExecutePlan creates a Google Slides presentation by duplicating template slides
// according to the plan, then applies text modifications with markdown formatting.
func ExecutePlan(ctx context.Context, plan *model.PresentationPlan, slidesSrv *slides.Service, driveSrv *drive.Service) (presId string, err error) {
	slog.Info("copying template", "templateID", plan.TemplateID)
	copiedFile, err := driveSrv.Files.Copy(plan.TemplateID, &drive.File{
		Name:    plan.PresentationTitle,
		Parents: []string{"root"},
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to copy template: %w", err)
	}
	presId = copiedFile.Id
	slog.Info("presentation created", "presentationID", presId)

	pres, err := slidesSrv.Presentations.Get(presId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to get presentation: %w", err)
	}

	pageMap := make(map[string]*slides.Page, len(pres.Slides))
	for _, page := range pres.Slides {
		pageMap[page.ObjectId] = page
	}

	refsByPlanIndex := make(map[int]model.SlideRef, len(plan.Slides))
	var dupRefs []model.SlideRef
	var dupRequests []*slides.Request
	dupCounter := 0

	for i, spec := range plan.Slides {
		srcId := spec.SourceSlideID
		srcPage, ok := pageMap[srcId]
		if !ok {
			slog.Warn("slide not found in presentation, skipping", "slideNumber", spec.SourceSlideNumber, "slideID", srcId)
			continue
		}

		dupCounter++
		objectIds := make(map[string]string)
		newPageId := fmt.Sprintf("d%d_%s", dupCounter, srcId)
		objectIds[srcId] = newPageId

		for _, elId := range islides.CollectElementIds(srcPage) {
			objectIds[elId] = fmt.Sprintf("d%d_%s", dupCounter, elId)
		}

		dupRequests = append(dupRequests, &slides.Request{
			DuplicateObject: &slides.DuplicateObjectRequest{
				ObjectId:  srcId,
				ObjectIds: objectIds,
			},
		})

		ref := model.SlideRef{PageObjectID: newPageId, ElementMap: objectIds}
		refsByPlanIndex[i] = ref
		dupRefs = append(dupRefs, ref)
	}

	if len(dupRequests) > 0 {
		slog.Info("duplicating slides", "count", len(dupRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: dupRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to duplicate slides: %w", err)
		}
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
		slog.Info("deleting original template slides", "count", len(deleteRequests))
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
	for i := len(dupRefs) - 1; i >= 0; i-- {
		reorderRequests = append(reorderRequests, &slides.Request{
			UpdateSlidesPosition: &slides.UpdateSlidesPositionRequest{
				SlideObjectIds:  []string{dupRefs[i].PageObjectID},
				InsertionIndex:  0,
				ForceSendFields: []string{"InsertionIndex"},
			},
		})
	}

	if len(reorderRequests) > 0 {
		slog.Info("reordering slides", "count", len(dupRefs))
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
		ref, ok := refsByPlanIndex[i]
		if !ok {
			continue
		}
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
		slog.Info("updating text elements", "count", len(updateRequests))
		_, err := slidesSrv.Presentations.BatchUpdate(presId, &slides.BatchUpdatePresentationRequest{
			Requests: updateRequests,
		}).Do()
		if err != nil {
			return "", fmt.Errorf("failed to update text content: %w", err)
		}
	}

	slog.Info("presentation complete", "url", fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", presId))
	return presId, nil
}
