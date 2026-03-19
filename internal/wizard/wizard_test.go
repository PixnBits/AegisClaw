package wizard

import (
	"encoding/json"
	"testing"
)

func TestComputedRisk_Low(t *testing.T) {
	r := &WizardResult{DataSensitivity: 1, NetworkExposure: 1, PrivilegeLevel: 1}
	if got := r.ComputedRisk(); got != "low" {
		t.Errorf("expected low, got %s", got)
	}
}

func TestComputedRisk_Medium(t *testing.T) {
	r := &WizardResult{DataSensitivity: 2, NetworkExposure: 3, PrivilegeLevel: 2}
	if got := r.ComputedRisk(); got != "medium" {
		t.Errorf("expected medium, got %s", got)
	}
}

func TestComputedRisk_High(t *testing.T) {
	r := &WizardResult{DataSensitivity: 4, NetworkExposure: 3, PrivilegeLevel: 4}
	if got := r.ComputedRisk(); got != "high" {
		t.Errorf("expected high, got %s", got)
	}
}

func TestComputedRisk_Critical(t *testing.T) {
	r := &WizardResult{DataSensitivity: 5, NetworkExposure: 5, PrivilegeLevel: 5}
	if got := r.ComputedRisk(); got != "critical" {
		t.Errorf("expected critical, got %s", got)
	}
}

func TestComputedRisk_Boundary(t *testing.T) {
	// avg = (1+1+3)/3 = 1.67 -> medium
	r := &WizardResult{DataSensitivity: 1, NetworkExposure: 1, PrivilegeLevel: 3}
	if got := r.ComputedRisk(); got != "medium" {
		t.Errorf("expected medium at boundary, got %s", got)
	}
}

func TestToProposalJSON_ValidOutput(t *testing.T) {
	r := &WizardResult{
		SkillName:   "slack-api",
		Description: "Slack integration skill",
		AllowedHosts:     []string{"api.slack.com"},
		AllowedPorts:     []uint16{443},
		AllowedProtocols: []string{"tcp"},
		SecretsRefs:      []string{"SLACK_TOKEN"},
		RequiredPersonas: []string{"CISO", "SeniorCoder"},
		Tools: []WizardToolSpec{
			{Name: "send_message", Description: "Send a Slack message"},
		},
	}

	data, err := r.ToProposalJSON()
	if err != nil {
		t.Fatalf("ToProposalJSON: %v", err)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if spec["name"] != "slack-api" {
		t.Errorf("expected name=slack-api, got %v", spec["name"])
	}
	if spec["language"] != "go" {
		t.Errorf("expected language=go, got %v", spec["language"])
	}
	if spec["entry_point"] != "cmd/slack-api/main.go" {
		t.Errorf("expected entry_point with skill name, got %v", spec["entry_point"])
	}

	np, ok := spec["network_policy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing network_policy")
	}
	if np["default_deny"] != true {
		t.Error("default_deny should be true")
	}

	tools, ok := spec["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", spec["tools"])
	}
}

func TestToProposalJSON_EmptyNetwork(t *testing.T) {
	r := &WizardResult{
		SkillName:   "standalone",
		Description: "No network skill",
		Tools: []WizardToolSpec{
			{Name: "process", Description: "Process data locally"},
		},
	}

	data, err := r.ToProposalJSON()
	if err != nil {
		t.Fatalf("ToProposalJSON: %v", err)
	}

	var spec map[string]interface{}
	json.Unmarshal(data, &spec)

	np := spec["network_policy"].(map[string]interface{})
	if np["default_deny"] != true {
		t.Error("default_deny should always be true")
	}
}

func TestToProposalJSON_MultipleTools(t *testing.T) {
	r := &WizardResult{
		SkillName:   "multi-tool",
		Description: "Skill with multiple tools",
		Tools: []WizardToolSpec{
			{Name: "read", Description: "Read data"},
			{Name: "write", Description: "Write data"},
			{Name: "delete", Description: "Delete data"},
		},
	}

	data, err := r.ToProposalJSON()
	if err != nil {
		t.Fatalf("ToProposalJSON: %v", err)
	}

	var spec map[string]interface{}
	json.Unmarshal(data, &spec)

	tools := spec["tools"].([]interface{})
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

func TestToNetworkPolicy_Structure(t *testing.T) {
	r := &WizardResult{
		AllowedHosts:     []string{"api.example.com", "10.0.0.1"},
		AllowedPorts:     []uint16{443, 8080},
		AllowedProtocols: []string{"tcp", "udp"},
	}

	np := r.ToNetworkPolicy()
	if np["default_deny"] != true {
		t.Error("default_deny must be true")
	}
	hosts := np["allowed_hosts"].([]string)
	if len(hosts) != 2 {
		t.Errorf("expected 2 hosts, got %d", len(hosts))
	}
}

func TestFormatSummary_WithNetwork(t *testing.T) {
	r := &WizardResult{
		Title:            "Add slack skill",
		SkillName:        "slack-api",
		Category:         "new_skill",
		DataSensitivity:  3,
		NetworkExposure:  3,
		PrivilegeLevel:   3,
		NeedsNetwork:     true,
		AllowedHosts:     []string{"api.slack.com"},
		AllowedPorts:     []uint16{443},
		AllowedProtocols: []string{"tcp"},
		RequiredPersonas: []string{"CISO"},
		Tools: []WizardToolSpec{
			{Name: "send", Description: "Send message"},
		},
	}

	summary := formatSummary(r)
	if len(summary) == 0 {
		t.Fatal("summary should not be empty")
	}
	if !contains(summary, "slack-api") {
		t.Error("summary missing skill name")
	}
	if !contains(summary, "api.slack.com") {
		t.Error("summary missing network host")
	}
}

func TestFormatSummary_NoNetwork(t *testing.T) {
	r := &WizardResult{
		Title:            "Add standalone skill",
		SkillName:        "standalone",
		Category:         "new_skill",
		DataSensitivity:  1,
		NetworkExposure:  1,
		PrivilegeLevel:   1,
		NeedsNetwork:     false,
		RequiredPersonas: []string{"SeniorCoder"},
		Tools: []WizardToolSpec{
			{Name: "process", Description: "Process data"},
		},
	}

	summary := formatSummary(r)
	if !contains(summary, "none (full isolation)") {
		t.Error("summary should indicate no network")
	}
}

func TestFormatSummary_WithSecrets(t *testing.T) {
	r := &WizardResult{
		Title:            "Secret skill",
		SkillName:        "secret-skill",
		Category:         "new_skill",
		DataSensitivity:  4,
		NetworkExposure:  2,
		PrivilegeLevel:   3,
		SecretsRefs:      []string{"API_KEY", "DB_PASSWORD"},
		RequiredPersonas: []string{"CISO"},
		Tools: []WizardToolSpec{
			{Name: "query", Description: "Query database"},
		},
	}

	summary := formatSummary(r)
	if !contains(summary, "API_KEY") {
		t.Error("summary missing secret refs")
	}
}

func TestHostRegex_Valid(t *testing.T) {
	valid := []string{"api.slack.com", "10.0.0.1", "example.com", "my-host.io", "localhost"}
	for _, h := range valid {
		if !hostRegex.MatchString(h) {
			t.Errorf("expected valid host: %q", h)
		}
	}
}

func TestHostRegex_Invalid(t *testing.T) {
	invalid := []string{"", "-bad.com", "spa ce.com", "../../etc/passwd"}
	for _, h := range invalid {
		if hostRegex.MatchString(h) {
			t.Errorf("expected invalid host: %q", h)
		}
	}
}

func TestSecretNameRegex_Valid(t *testing.T) {
	valid := []string{"API_KEY", "slack_token", "MySecret123", "a"}
	for _, s := range valid {
		if !secretNameRegex.MatchString(s) {
			t.Errorf("expected valid secret name: %q", s)
		}
	}
}

func TestSecretNameRegex_Invalid(t *testing.T) {
	invalid := []string{"", "123abc", "-bad", "has space"}
	for _, s := range invalid {
		if secretNameRegex.MatchString(s) {
			t.Errorf("expected invalid secret name: %q", s)
		}
	}
}

func TestSkillNameRegex_Valid(t *testing.T) {
	valid := []string{"slack-api", "redis", "my-skill-123", "ab"}
	for _, s := range valid {
		if !skillNameRegex.MatchString(s) {
			t.Errorf("expected valid skill name: %q", s)
		}
	}
}

func TestSkillNameRegex_Invalid(t *testing.T) {
	invalid := []string{"", "A", "-bad", "Has Space", "123abc"}
	for _, s := range invalid {
		if skillNameRegex.MatchString(s) {
			t.Errorf("expected invalid skill name: %q", s)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
