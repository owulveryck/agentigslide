package outliner

import (
	"fmt"
	"regexp"
	"strings"
)

var slideMarkerRe = regexp.MustCompile(`(?im)^##\s+SLIDE\s+\d+\s*[—\-:]\s*`)

const structuredThreshold = 3

func detectStructuredInput(userRequest string) bool {
	return len(slideMarkerRe.FindAllStringIndex(userRequest, -1)) >= structuredThreshold
}

func buildStructuredUserMessage(userRequest string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `La demande ci-dessous est DÉJÀ STRUCTURÉE slide par slide.

INSTRUCTIONS IMPÉRATIVES :
- Produis EXACTEMENT une SlideNeed par slide décrite (même nombre, même ordre).
- Ne fusionne PAS de slides entre elles.
- Ne découpe PAS une slide en plusieurs.
- Ne réorganise PAS l'ordre des slides.
- Extrais le contenu textuel de chaque slide tel quel, sans le reformuler ni le résumer.
- Classifie chaque slide par type (cover, content, data, conclusion, section_divider, diagram).
- Compte précisément itemCount et maxItemLength pour chaque slide.

Demande de l'utilisateur :

%s`, userRequest)
	return b.String()
}
