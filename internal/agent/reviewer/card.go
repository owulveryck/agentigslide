package reviewer

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/owulveryck/agentigslide/internal/agent"
)

// Card returns the A2A AgentCard describing this agent's capabilities.
func Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:        "Reviewer",
		Description: "Valide un plan de présentation assemblé contre des règles de qualité : débordement de contenu, doublons, champs manquants, incohérences avec la demande utilisateur, et contenu inventé.",
		Version:     agent.AgentVersion,
		Provider:    agent.DefaultProvider(),
		DefaultInputModes:  agent.DefaultInputModes(),
		DefaultOutputModes: agent.DefaultOutputModes(),
		Skills: []a2a.AgentSkill{
			{
				ID:          "review",
				Name:        "Quality Review",
				Description: "Vérifie un plan de présentation assemblé et signale les problèmes de qualité.",
				Tags:        []string{"presentation", "review", "quality", "validation"},
			},
		},
	}
}
