#!/usr/bin/env python3
"""Writes internal/builder/codegen_test.go — unit tests for SkillSpec + CodeGenerator."""
import os

code = r'''package builder

import (
	"testing"
)

func validSkillSpec() SkillSpec {
	return SkillSpec{
		Name:        "test-skill",
		Description: "A test skill for unit testing",
		Tools: []ToolSpec{
			{
				Name:        "greet",
				Description: "Returns a greeting",
				InputSchema: `{"type": "object", "properties": {"name": {"type": "string"}}}`,
				OutputSchema: `{"type": "object", "properties": {"message": {"type": "string"}}}`,
			},
		},
		NetworkPolicy: SkillNetworkPolicy{
			DefaultDeny: true,
		},
		Language:   "go",
		EntryPoint: "main.go",
	}
}

func TestSkillSpecValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*SkillSpec)
		wantErr string
	}{
		{
			name:    "valid spec",
			modify:  func(s *SkillSpec) {},
			wantErr: "",
		},
		{
			name:    "empty name",
			modify:  func(s *SkillSpec) { s.Name = "" },
			wantErr: "skill name is required",
		},
		{
			name:    "invalid name pattern",
			modify:  func(s *SkillSpec) { s.Name = "Invalid-Name" },
			wantErr: "skill name must match",
		},
		{
			name:    "name with spaces",
			modify:  func(s *SkillSpec) { s.Name = "has space" },
			wantErr: "skill name must match",
		},
		{
			name:    "empty description",
			modify:  func(s *SkillSpec) { s.Description = "" },
			wantErr: "skill description is required",
		},
		{
			name: "description too long",
			modify: func(s *SkillSpec) {
				s.Description = string(make([]byte, 2049))
			},
			wantErr: "skill description must be <= 2048 chars",
		},
		{
			name:    "no tools",
			modify:  func(s *SkillSpec) { s.Tools = nil },
			wantErr: "at least one tool is required",
		},
		{
			name: "tool missing name",
			modify: func(s *SkillSpec) {
				s.Tools = []ToolSpec{{Description: "test"}}
			},
			wantErr: "tool[0] name is required",
		},
		{
			name: "tool missing description",
			modify: func(s *SkillSpec) {
				s.Tools = []ToolSpec{{Name: "test"}}
			},
			wantErr: "tool[0] description is required",
		},
		{
			name:    "unsupported language",
			modify:  func(s *SkillSpec) { s.Language = "python" },
			wantErr: "only Go language is supported",
		},
		{
			name:    "empty entry point",
			modify:  func(s *SkillSpec) { s.EntryPoint = "" },
			wantErr: "entry point is required",
		},
		{
			name:    "network policy deny false",
			modify:  func(s *SkillSpec) { s.NetworkPolicy.DefaultDeny = false },
			wantErr: "network policy default_deny must be true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := validSkillSpec()
			tt.modify(&spec)
			err := spec.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestSkillSpecLanguageDefault(t *testing.T) {
	spec := validSkillSpec()
	spec.Language = ""
	err := spec.Validate()
	if err != nil {
		t.Errorf("expected no error for empty language (should default to go), got %v", err)
	}
	if spec.Language != "go" {
		t.Errorf("expected language to default to go, got %q", spec.Language)
	}
}

func TestCodeGenRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     CodeGenRequest
		wantErr string
	}{
		{
			name: "valid request",
			req: CodeGenRequest{
				Spec:         validSkillSpec(),
				Round:        1,
				SystemPrompt: "Generate code",
				MaxTokens:    4096,
			},
			wantErr: "",
		},
		{
			name: "round too low",
			req: CodeGenRequest{
				Spec:         validSkillSpec(),
				Round:        0,
				SystemPrompt: "Generate code",
			},
			wantErr: "round must be between 1 and 3",
		},
		{
			name: "round too high",
			req: CodeGenRequest{
				Spec:         validSkillSpec(),
				Round:        4,
				SystemPrompt: "Generate code",
			},
			wantErr: "round must be between 1 and 3",
		},
		{
			name: "empty system prompt",
			req: CodeGenRequest{
				Spec:  validSkillSpec(),
				Round: 1,
			},
			wantErr: "system prompt is required",
		},
		{
			name: "max tokens defaults",
			req: CodeGenRequest{
				Spec:         validSkillSpec(),
				Round:        1,
				SystemPrompt: "Generate code",
				MaxTokens:    0,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestCodeGenResponseValidation(t *testing.T) {
	tests := []struct {
		name    string
		resp    CodeGenResponse
		wantErr string
	}{
		{
			name: "valid response",
			resp: CodeGenResponse{
				Files: map[string]string{"main.go": "package main"},
			},
			wantErr: "",
		},
		{
			name:    "no files",
			resp:    CodeGenResponse{},
			wantErr: "no files generated",
		},
		{
			name: "empty path",
			resp: CodeGenResponse{
				Files: map[string]string{"": "content"},
			},
			wantErr: "empty file path",
		},
		{
			name: "path traversal",
			resp: CodeGenResponse{
				Files: map[string]string{"../../../etc/passwd": "evil"},
			},
			wantErr: "path traversal detected",
		},
		{
			name: "empty content",
			resp: CodeGenResponse{
				Files: map[string]string{"main.go": ""},
			},
			wantErr: "empty content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.resp.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestPromptTemplateFormat(t *testing.T) {
	tmpl := &PromptTemplate{
		Name:   "test",
		System: "You are building {{skill_name}} skill",
		User:   "Generate code for {{skill_name}} with {{tool_count}} tools",
	}

	vars := map[string]string{
		"skill_name": "my-skill",
		"tool_count": "3",
	}

	system, user := tmpl.Format(vars)

	expectedSystem := "You are building my-skill skill"
	expectedUser := "Generate code for my-skill with 3 tools"

	if system != expectedSystem {
		t.Errorf("system mismatch:\n  got:  %q\n  want: %q", system, expectedSystem)
	}
	if user != expectedUser {
		t.Errorf("user mismatch:\n  got:  %q\n  want: %q", user, expectedUser)
	}
}

func TestDefaultTemplates(t *testing.T) {
	templates := DefaultTemplates()

	expected := []string{"skill_codegen", "skill_edit", "skill_fix"}
	for _, name := range expected {
		tmpl, ok := templates[name]
		if !ok {
			t.Errorf("missing template: %s", name)
			continue
		}
		if tmpl.Name != name {
			t.Errorf("template name mismatch: %s != %s", tmpl.Name, name)
		}
		if tmpl.System == "" {
			t.Errorf("template %s has empty system prompt", name)
		}
		if tmpl.User == "" {
			t.Errorf("template %s has empty user prompt", name)
		}
		if tmpl.Description == "" {
			t.Errorf("template %s has empty description", name)
		}
	}
}

func TestNewCodeGeneratorValidation(t *testing.T) {
	templates := DefaultTemplates()

	_, err := NewCodeGenerator(nil, nil, nil, templates)
	if err == nil || !containsStr(err.Error(), "builder runtime is required") {
		t.Errorf("expected builder runtime error, got %v", err)
	}

	_, err = NewCodeGenerator(nil, nil, nil, nil)
	if err == nil || !containsStr(err.Error(), "builder runtime is required") {
		t.Errorf("expected builder runtime error, got %v", err)
	}
}

func TestSkillNamePatterns(t *testing.T) {
	valid := []string{"my-skill", "abc", "test_skill_123", "a1", "ab"}
	invalid := []string{"", "A", "-start", "_start", "has space", "a"}

	for _, name := range valid {
		if !skillNameRegex.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}
	for _, name := range invalid {
		if skillNameRegex.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestToolSpecMultiple(t *testing.T) {
	spec := validSkillSpec()
	spec.Tools = append(spec.Tools, ToolSpec{
		Name:        "farewell",
		Description: "Returns a farewell message",
	})
	if err := spec.Validate(); err != nil {
		t.Errorf("expected valid spec with 2 tools, got %v", err)
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'builder', 'codegen_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"codegen_test.go: {len(code)} bytes -> {outpath}")
