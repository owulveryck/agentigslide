package outliner

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/owulveryck/agentigslide/internal/agent"
)

// Card returns the A2A AgentCard describing this agent's capabilities.
func Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:               "Outliner",
		Description:        "Analyse une demande utilisateur et produit un plan de présentation structuré (PresentationOutline) avec sections, besoins de slides, et métadonnées de contenu.",
		Version:            agent.AgentVersion,
		Provider:           agent.DefaultProvider(),
		DefaultInputModes:  agent.DefaultInputModes(),
		DefaultOutputModes: agent.DefaultOutputModes(),
		Skills: []a2a.AgentSkill{
			{
				ID:          "outline",
				Name:        "Outline Generation",
				Description: "Produit un plan structuré à partir d'une demande de présentation en langage naturel ou markdown.",
				Tags:        []string{"presentation", "outline", "structure"},
				Examples: []string{
					"Crée une présentation sur l'IA générative en 10 slides",
					"Prépare un deck de 5 slides pour le comité de direction sur les résultats Q1",
				},
			},
		},
	}
}
