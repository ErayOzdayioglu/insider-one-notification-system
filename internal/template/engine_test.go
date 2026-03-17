package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_Render(t *testing.T) {
	eng := NewEngine()

	tests := []struct {
		name      string
		content   string
		vars      map[string]string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "all variables provided",
			content: "Hello {{.Name}}, your code is {{.Code}}.",
			vars:    map[string]string{"Name": "Alice", "Code": "1234"},
			want:    "Hello Alice, your code is 1234.",
		},
		{
			name:    "single variable",
			content: "Welcome {{.User}}!",
			vars:    map[string]string{"User": "Bob"},
			want:    "Welcome Bob!",
		},
		{
			name:    "no variables in template",
			content: "Static content without placeholders.",
			vars:    map[string]string{},
			want:    "Static content without placeholders.",
		},
		{
			name:      "missing variable causes error",
			content:   "Hello {{.Name}}, welcome!",
			vars:      map[string]string{},
			wantErr:   true,
			errSubstr: "executing template",
		},
		{
			name:    "extra variables are ignored",
			content: "Hello {{.Name}}!",
			vars:    map[string]string{"Name": "Alice", "Extra": "ignored"},
			want:    "Hello Alice!",
		},
		{
			name:    "complex template with multiple variables",
			content: "Dear {{.FirstName}} {{.LastName}}, your order #{{.OrderID}} for {{.Product}} is confirmed. Total: {{.Amount}}.",
			vars: map[string]string{
				"FirstName": "Jane",
				"LastName":  "Doe",
				"OrderID":   "ORD-999",
				"Product":   "Widget",
				"Amount":    "$42.00",
			},
			want: "Dear Jane Doe, your order #ORD-999 for Widget is confirmed. Total: $42.00.",
		},
		{
			name:      "invalid template syntax",
			content:   "Hello {{.Name",
			vars:      map[string]string{"Name": "Alice"},
			wantErr:   true,
			errSubstr: "parsing template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := eng.Render(tt.content, tt.vars)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestEngine_ValidateVariables(t *testing.T) {
	eng := NewEngine()

	tests := []struct {
		name     string
		required []string
		provided map[string]string
		wantErr  bool
		errParts []string
	}{
		{
			name:     "all present",
			required: []string{"Name", "Code"},
			provided: map[string]string{"Name": "Alice", "Code": "1234"},
			wantErr:  false,
		},
		{
			name:     "empty required is valid",
			required: []string{},
			provided: map[string]string{},
			wantErr:  false,
		},
		{
			name:     "extra provided is fine",
			required: []string{"Name"},
			provided: map[string]string{"Name": "Alice", "Extra": "ok"},
			wantErr:  false,
		},
		{
			name:     "single missing variable",
			required: []string{"Name"},
			provided: map[string]string{},
			wantErr:  true,
			errParts: []string{"Name"},
		},
		{
			name:     "multiple missing variables",
			required: []string{"Name", "Code", "Email"},
			provided: map[string]string{"Name": "Alice"},
			wantErr:  true,
			errParts: []string{"Code", "Email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eng.ValidateVariables(tt.required, tt.provided)
			if tt.wantErr {
				require.Error(t, err)
				for _, part := range tt.errParts {
					assert.Contains(t, err.Error(), part)
				}
				return
			}
			assert.NoError(t, err)
		})
	}
}
