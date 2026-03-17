package template

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateEngine defines the contract for rendering notification content
// from templates and validating that required variables are provided.
type TemplateEngine interface {
	Render(templateContent string, variables map[string]string) (string, error)
	ValidateVariables(required []string, provided map[string]string) error
}

// engine is the default TemplateEngine implementation backed by Go's
// text/template package.
type engine struct{}

// NewEngine creates a new TemplateEngine instance.
func NewEngine() TemplateEngine {
	return &engine{}
}

// Render parses the given template string and executes it against the
// supplied variables. It returns the rendered output or an error if
// parsing or execution fails. Missing variables cause an error thanks
// to the "missingkey=error" option.
func (e *engine) Render(templateContent string, variables map[string]string) (string, error) {
	tmpl, err := template.New("notification").
		Option("missingkey=error").
		Parse(templateContent)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, variables); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// ValidateVariables checks that every name in required has a corresponding
// entry in provided. It returns a descriptive error listing all missing
// variable names, or nil when everything is present.
func (e *engine) ValidateVariables(required []string, provided map[string]string) error {
	var missing []string
	for _, name := range required {
		if _, ok := provided[name]; !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required template variables: %s", strings.Join(missing, ", "))
	}

	return nil
}
