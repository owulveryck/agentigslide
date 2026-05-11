package orchestrator

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/owulveryck/agentigslide/internal/agent"
)

// Card returns the A2A AgentCard describing the orchestrator's capabilities.
func Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:               "Orchestrator",
		Description:        "Orchestre le pipeline complet de génération de présentations Google Slides : analyse de la demande, sélection des templates, génération du contenu, revue qualité, et création de la présentation finale.",
		Version:            agent.AgentVersion,
		Provider:           agent.DefaultProvider(),
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{
			{
				ID:          "generate",
				Name:        "Presentation Generation",
				Description: "Génère une présentation Google Slides complète à partir d'une demande en langage naturel ou markdown. Retourne l'URL de la présentation créée.",
				Tags:        []string{"presentation", "google-slides", "generation", "pipeline"},
				Examples: []string{
					"Crée une présentation sur l'IA générative en 10 slides",
					"Prépare un deck de 5 slides pour le comité de direction sur les résultats Q1",
				},
			},
		},
	}
}
