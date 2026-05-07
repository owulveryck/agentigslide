package templateindex

import (
	"testing"

	"github.com/owulveryck/agentigslide/internal/model"
)

func TestInferRole(t *testing.T) {
	subtitle := "SUBTITLE"
	body := "BODY"
	title := "TITLE"

	tests := []struct {
		name string
		elem model.EditableElement
		want string
	}{
		{
			name: "titre principal in description",
			elem: model.EditableElement{Description: "Le titre principal de la slide"},
			want: "titre_principal",
		},
		{
			name: "titre de la slide",
			elem: model.EditableElement{Description: "titre de la slide avec logo"},
			want: "titre_principal",
		},
		{
			name: "sous-titre",
			elem: model.EditableElement{Description: "Sous-titre de la section"},
			want: "sous_titre",
		},
		{
			name: "SUBTITLE placeholder",
			elem: model.EditableElement{Description: "texte", Placeholder: &subtitle},
			want: "sous_titre",
		},
		{
			name: "sommaire in description",
			elem: model.EditableElement{Description: "Le sommaire du deck"},
			want: "sommaire",
		},
		{
			name: "sommaire in content",
			elem: model.EditableElement{Description: "texte", Content: "Sommaire général"},
			want: "sommaire",
		},
		{
			name: "annee with year in content",
			elem: model.EditableElement{Description: "champ de texte", Content: "2026"},
			want: "annee",
		},
		{
			name: "entreprise with octo in content",
			elem: model.EditableElement{Description: "nom", Content: "OCTO Technology"},
			want: "entreprise",
		},
		{
			name: "copyright in description",
			elem: model.EditableElement{Description: "mention copyright en bas"},
			want: "copyright",
		},
		{
			name: "copyright symbol in content",
			elem: model.EditableElement{Description: "mention légale", Content: "© Tous droits réservés"},
			want: "copyright",
		},
		{
			name: "numero de page",
			elem: model.EditableElement{Description: "numéro de page en bas"},
			want: "numero_page",
		},
		{
			name: "pagination",
			elem: model.EditableElement{Description: "champ de pagination"},
			want: "numero_page",
		},
		{
			name: "numerotation",
			elem: model.EditableElement{Description: "numérotation des étapes"},
			want: "numerotation",
		},
		{
			name: "bullet list",
			elem: model.EditableElement{Description: "liste de points clés"},
			want: "liste_points",
		},
		{
			name: "tableau",
			elem: model.EditableElement{Description: "cellule du tableau"},
			want: "tableau",
		},
		{
			name: "legende",
			elem: model.EditableElement{Description: "légende de l'image"},
			want: "legende",
		},
		{
			name: "caption",
			elem: model.EditableElement{Description: "caption for the diagram"},
			want: "legende",
		},
		{
			name: "BODY placeholder",
			elem: model.EditableElement{Description: "zone de contenu", Placeholder: &body},
			want: "corps_texte",
		},
		{
			name: "TITLE placeholder",
			elem: model.EditableElement{Description: "en-tête", Placeholder: &title},
			want: "titre",
		},
		{
			name: "default texte",
			elem: model.EditableElement{Description: "une forme avec du contenu"},
			want: "texte",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferRole(tt.elem)
			if got != tt.want {
				t.Errorf("InferRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsPlaceholderContent(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"Lorem ipsum dolor sit amet", true},
		{"  LOREM IPSUM  ", true},
		{"This is dummy text for testing", true},
		{"Real presentation content", false},
		{"", false},
		{"dolor sit amet consectetur", true},
	}

	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			got := IsPlaceholderContent(tt.content)
			if got != tt.want {
				t.Errorf("IsPlaceholderContent(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestRoleToVariablePrefix(t *testing.T) {
	tests := []struct {
		role string
		want string
	}{
		{"titre_principal", "titleMain"},
		{"sous_titre", "subtitle"},
		{"annee", "year"},
		{"entreprise", "company"},
		{"texte", "text"},
		{"unknown_role", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := RoleToVariablePrefix(tt.role)
			if got != tt.want {
				t.Errorf("RoleToVariablePrefix(%q) = %q, want %q", tt.role, got, tt.want)
			}
		})
	}
}
