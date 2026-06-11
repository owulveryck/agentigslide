package selector

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/owulveryck/agentigslide/internal/agent"
)

// Card returns the A2A AgentCard describing this agent's capabilities.
func Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:               "Selector",
		Description:        "Mappe chaque besoin de slide d'un plan de présentation au template le plus adapté du catalogue, en tenant compte du nombre de champs, des types de contenu et du style visuel.",
		Version:            agent.AgentVersion,
		Provider:           agent.DefaultProvider(),
		DefaultInputModes:  agent.DefaultInputModes(),
		DefaultOutputModes: agent.DefaultOutputModes(),
		Skills: []a2a.AgentSkill{
			{
				ID:          "select",
				Name:        "Template Selection",
				Description: "Sélectionne les templates les plus adaptés pour chaque slide d'un plan de présentation structuré.",
				Tags:        []string{"presentation", "template", "selection", "mapping"},
			},
		},
	}
}
