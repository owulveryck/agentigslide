package writer

import (
	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/owulveryck/agentigslide/internal/agent"
)

// Card returns the A2A AgentCard describing this agent's capabilities.
func Card() a2a.AgentCard {
	return a2a.AgentCard{
		Name:        "Writer",
		Description: "Génère le contenu textuel pour un slide en mappant les éléments de contenu du plan aux champs éditables du template, en respectant les limites de caractères et le formatage markdown.",
		Version:     agent.AgentVersion,
		Provider:    agent.DefaultProvider(),
		DefaultInputModes:  agent.DefaultInputModes(),
		DefaultOutputModes: agent.DefaultOutputModes(),
		Skills: []a2a.AgentSkill{
			{
				ID:          "write",
				Name:        "Slide Content Writing",
				Description: "Produit le contenu textuel pour chaque champ éditable d'un slide template.",
				Tags:        []string{"presentation", "content", "writing", "slide"},
			},
		},
	}
}
