// Command generateSlideList generates a structured presentation plan JSON from
// a user request. It loads the template index, sends the user request along
// with a compact template description to Claude via Vertex AI, and outputs an
// enriched PresentationPlan to stdout.
//
// Usage:
//
//	go run generateSlideList/generate_slide_list.go --request "Create a deck about innovation"
//	go run generateSlideList/generate_slide_list.go --interactive
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/owulveryck/slideAppScripter/internal/config"
	"github.com/owulveryck/slideAppScripter/internal/model"
	"github.com/owulveryck/slideAppScripter/internal/plan"
	"github.com/owulveryck/slideAppScripter/internal/vertex"

	"github.com/kelseyhightower/envconfig"
)

type genslidesConfig struct {
	Model string `envconfig:"MODEL" default:"claude-sonnet-4-5@20250929" desc:"Claude model for plan generation"`
}

func main() {
	interactive := flag.Bool("interactive", false, "Interactive mode (read from stdin)")
	request := flag.String("request", "", "User request for slide generation")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: generate_slide_list --request \"your request\" OR --interactive\n\nFlags:\n")
		flag.PrintDefaults()
		config.PrintAllUsage(
			struct {
				Prefix string
				Spec   any
			}{"SLIDES", &config.SlidesConfig{}},
			struct {
				Prefix string
				Spec   any
			}{"VERTEX", &vertex.Config{}},
			struct {
				Prefix string
				Spec   any
			}{"GENSLIDES", &genslidesConfig{}},
		)
	}
	flag.Parse()

	var userRequest string
	if *interactive {
		fmt.Fprintln(os.Stderr, "Enter your slide generation request:")
		var input bytes.Buffer
		_, _ = io.Copy(&input, os.Stdin)
		userRequest = input.String()
	} else if *request != "" {
		userRequest = *request
	} else {
		flag.Usage()
		os.Exit(1)
	}

	slidesCfg, err := config.LoadSlidesConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	vertexCfg, err := vertex.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	var gsCfg genslidesConfig
	if err := envconfig.Process("GENSLIDES", &gsCfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	index, err := plan.LoadTemplateIndex(slidesCfg.TemplateIndex)
	if err != nil {
		log.Fatalf("Failed to load template index: %v\nPlease run 'go run buildTemplateIndex/build_template_index.go' first", err)
	}

	ctx := context.Background()
	vc, err := vertex.NewClient(ctx, vertexCfg)
	if err != nil {
		log.Fatalf("Failed to create Vertex AI client: %v", err)
	}

	compactIndex := plan.BuildCompactIndex(index)

	genPlan, err := parseUserRequest(ctx, vc, gsCfg.Model, userRequest, compactIndex)
	if err != nil {
		log.Fatalf("Failed to parse user request: %v", err)
	}

	output := plan.EnrichPlan(genPlan, index, slidesCfg.TemplateID, userRequest)

	result, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}
	fmt.Println(string(result))
}

func parseUserRequest(ctx context.Context, vc *vertex.Client, modelName, userRequest, templateIndexJSON string) (*model.GenerationPlan, error) {
	prompt := fmt.Sprintf(`Tu es un expert en création de présentations professionnelles à partir du template OCTO.

RÈGLES FONDAMENTALES :
1. N'INVENTE AUCUNE INFORMATION. Tout le contenu texte doit provenir exclusivement de la demande utilisateur. Si une information n'est pas dans la demande, ne la fabrique pas.
2. ADÉQUATION STRUCTURE/CONTENU : Le choix de chaque slide est dicté par le nombre d'informations à afficher. Compte les éléments de contenu disponibles dans la demande (bullet points, paragraphes, chiffres clés) et choisis une slide dont le nombre de zones éditables correspond. Par exemple : 3 points à afficher → slide avec 3 zones de contenu, PAS une slide avec 6 zones. Ne duplique JAMAIS du contenu pour remplir des zones vides. Préfère une slide plus simple plutôt qu'une slide trop riche avec des champs laissés vides ou répétés.
2bis. ADÉQUATION TAILLE/CONTENU : Chaque champ éditable indique sa capacité approximative en caractères (~N car.). Place les textes longs dans les grands champs et les textes courts dans les petits champs. Ne mets JAMAIS un texte de plus de N caractères dans un champ indiqué ~N car. Si le texte est trop long pour le champ disponible, résume-le ou choisis une slide avec des champs plus grands.
3. La présentation doit être cohérente et compréhensible : les slides intercalaires (titres de section, séparateurs) doivent être placées entre les parties qu'elles introduisent.
4. L'ordre des slides dans le JSON = l'ordre final dans la présentation.

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

	responseText, err := vc.RawPredict(ctx, modelName, messages)
	if err != nil {
		return nil, fmt.Errorf("claude API call failed: %w", err)
	}

	var plan model.GenerationPlan
	if err := json.Unmarshal([]byte(responseText), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan: %w\nResponse was: %s", err, responseText)
	}

	return &plan, nil
}
