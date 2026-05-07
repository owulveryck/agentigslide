package pipeline

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed prompt_default.txt.tmpl
var defaultPromptRaw string

//go:embed prompt_amend.txt.tmpl
var amendPromptRaw string

var (
	defaultPromptTmpl *template.Template
	amendPromptTmpl   *template.Template
)

func init() {
	if err := validateTemplate(defaultPromptRaw, defaultRequiredFields); err != nil {
		panic(fmt.Sprintf("prompt_default.txt.tmpl: %v", err))
	}
	if err := validateTemplate(amendPromptRaw, amendRequiredFields); err != nil {
		panic(fmt.Sprintf("prompt_amend.txt.tmpl: %v", err))
	}
	defaultPromptTmpl = template.Must(template.New("default").Parse(defaultPromptRaw))
	amendPromptTmpl = template.Must(template.New("amend").Parse(amendPromptRaw))
}

var defaultRequiredFields = []string{"TemplateIndex", "UserRequest"}
var amendRequiredFields = []string{"ExistingPlan", "TemplateIndex", "AmendmentRequest"}

func validateTemplate(content string, requiredFields []string) error {
	for _, field := range requiredFields {
		marker := "{{." + field + "}}"
		if !strings.Contains(content, marker) {
			return fmt.Errorf("missing required field %s", marker)
		}
	}
	return nil
}
