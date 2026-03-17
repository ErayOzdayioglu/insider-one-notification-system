package entity

import (
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// templateVarPattern matches Go template-style placeholders such as {{.VariableName}}.
var templateVarPattern = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

// Template defines a reusable notification template with variable placeholders.
// Content uses Go template syntax (e.g. {{.Name}}).
type Template struct {
	ID        uuid.UUID
	Name      string
	Channel   Channel
	Subject   *string
	Content   string
	Variables []string // required variable names extracted or declared
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTemplate creates a Template with a generated UUID and defaults.
func NewTemplate(name string, channel Channel, content string) *Template {
	now := time.Now().UTC()
	return &Template{
		ID:        uuid.New(),
		Name:      name,
		Channel:   channel,
		Content:   content,
		Variables: ExtractVariables(content),
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate checks that the template's fields satisfy all business rules.
func (t *Template) Validate() []error {
	var errs []error

	if t.ID == uuid.Nil {
		errs = append(errs, fmt.Errorf("id must not be nil"))
	}
	if t.Name == "" {
		errs = append(errs, fmt.Errorf("name must not be empty"))
	}
	if !t.Channel.IsValid() {
		errs = append(errs, fmt.Errorf("channel %q is not valid", t.Channel))
	}
	if t.Content == "" {
		errs = append(errs, fmt.Errorf("content must not be empty"))
	}
	if t.Channel == ChannelEmail && (t.Subject == nil || *t.Subject == "") {
		errs = append(errs, fmt.Errorf("subject is required for email templates"))
	}

	// Ensure declared variables actually appear in the content.
	contentVars := extractVariableSet(t.Content)
	for _, v := range t.Variables {
		if _, ok := contentVars[v]; !ok {
			errs = append(errs, fmt.Errorf("declared variable %q not found in content", v))
		}
	}

	return errs
}

// ExtractVariables parses template content and returns the unique variable
// names found in {{.Variable}} placeholders.
func ExtractVariables(content string) []string {
	return uniqueKeys(extractVariableSet(content))
}

func extractVariableSet(content string) map[string]struct{} {
	matches := templateVarPattern.FindAllStringSubmatch(content, -1)
	set := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		set[m[1]] = struct{}{}
	}
	return set
}

func uniqueKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
