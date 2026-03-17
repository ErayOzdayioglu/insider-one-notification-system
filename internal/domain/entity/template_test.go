package entity

import (
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplate_Validate(t *testing.T) {
	tests := []struct {
		name      string
		template  func() *Template
		wantErrs  int
		errSubstr string
	}{
		{
			name: "valid SMS template",
			template: func() *Template {
				return NewTemplate("welcome-sms", ChannelSMS, "Hello {{.Name}}, welcome!")
			},
			wantErrs: 0,
		},
		{
			name: "valid email template with subject",
			template: func() *Template {
				tmpl := NewTemplate("welcome-email", ChannelEmail, "Hello {{.Name}}, welcome!")
				subject := "Welcome {{.Name}}"
				tmpl.Subject = &subject
				return tmpl
			},
			wantErrs: 0,
		},
		{
			name: "missing name",
			template: func() *Template {
				tmpl := NewTemplate("", ChannelSMS, "Hello {{.Name}}")
				return tmpl
			},
			wantErrs:  1,
			errSubstr: "name",
		},
		{
			name: "missing content",
			template: func() *Template {
				tmpl := &Template{
					ID:       uuid.New(),
					Name:     "test",
					Channel:  ChannelSMS,
					Content:  "",
					IsActive: true,
				}
				return tmpl
			},
			wantErrs:  1,
			errSubstr: "content",
		},
		{
			name: "email template requires subject",
			template: func() *Template {
				tmpl := NewTemplate("email-no-subject", ChannelEmail, "Hello {{.Name}}")
				tmpl.Subject = nil
				return tmpl
			},
			wantErrs:  1,
			errSubstr: "subject",
		},
		{
			name: "email template with empty subject string",
			template: func() *Template {
				tmpl := NewTemplate("email-empty-subject", ChannelEmail, "Hello {{.Name}}")
				empty := ""
				tmpl.Subject = &empty
				return tmpl
			},
			wantErrs:  1,
			errSubstr: "subject",
		},
		{
			name: "declared variable not in content",
			template: func() *Template {
				tmpl := NewTemplate("mismatch", ChannelSMS, "Hello world")
				tmpl.Variables = []string{"Name"}
				return tmpl
			},
			wantErrs:  1,
			errSubstr: "not found in content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl := tt.template()
			errs := tmpl.Validate()
			if tt.wantErrs == 0 {
				assert.Empty(t, errs, "expected no validation errors")
			} else {
				require.NotEmpty(t, errs, "expected validation errors")
				assert.GreaterOrEqual(t, len(errs), tt.wantErrs)
				found := false
				for _, e := range errs {
					if containsSubstring(e.Error(), tt.errSubstr) {
						found = true
					}
				}
				assert.True(t, found, "expected error containing %q, got %v", tt.errSubstr, errs)
			}
		})
	}
}

func TestExtractVariables(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "single variable",
			content: "Hello {{.Name}}!",
			want:    []string{"Name"},
		},
		{
			name:    "multiple unique variables",
			content: "Hi {{.FirstName}} {{.LastName}}, your code is {{.Code}}",
			want:    []string{"Code", "FirstName", "LastName"},
		},
		{
			name:    "duplicate variable returns unique",
			content: "{{.Name}} is {{.Name}}",
			want:    []string{"Name"},
		},
		{
			name:    "no variables",
			content: "Plain text without variables",
			want:    []string{},
		},
		{
			name:    "variables with spaces around dot",
			content: "Hello {{ .Name }}!",
			want:    []string{"Name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractVariables(tt.content)
			sort.Strings(got)
			sort.Strings(tt.want)
			if len(tt.want) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestNewTemplate(t *testing.T) {
	tmpl := NewTemplate("test-tmpl", ChannelPush, "Hello {{.User}}")
	assert.NotEqual(t, uuid.Nil, tmpl.ID)
	assert.Equal(t, "test-tmpl", tmpl.Name)
	assert.Equal(t, ChannelPush, tmpl.Channel)
	assert.Equal(t, "Hello {{.User}}", tmpl.Content)
	assert.True(t, tmpl.IsActive)
	assert.Contains(t, tmpl.Variables, "User")
}
