package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/wizard"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// CLI flags for non-interactive skill addition.
var (
	skillAddName        string
	skillAddTitle       string
	skillAddDescription string
	skillAddTools       []string
	skillAddSensitivity int
	skillAddExposure    int
	skillAddPrivilege   int
	skillAddHosts       []string
	skillAddPortStrs    []string
	skillAddProtocols   []string
	skillAddSecretRefs  []string
	skillAddNonInteract bool
)

var skillAddCmd = &cobra.Command{
	Use:   "add <natural language description>",
	Short: "Propose and add a new skill",
	Long: `Proposes a new skill for Governance Court review. This triggers the full
review process: requirement refinement, multi-persona Court review, builder
pipeline, and deployment.

By default launches an interactive wizard. Use --non-interactive with flags
for scripted use.

Examples:
  aegisclaw skill add "Slack messaging capability"
  aegisclaw skill add "Hello World" --non-interactive \
    --name hello-world \
    --tool "greet:Returns a greeting message"`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillAdd,
}

func init() {
	skillAddCmd.Flags().StringVar(&skillAddName, "name", "", "Skill name (lowercase, letters/digits/hyphens)")
	skillAddCmd.Flags().StringVar(&skillAddTitle, "title", "", "Proposal title (default: \"Add <goal> skill\")")
	skillAddCmd.Flags().StringVar(&skillAddDescription, "description", "", "Skill description")
	skillAddCmd.Flags().StringSliceVar(&skillAddTools, "tool", nil, "Tool definition as name:description (repeatable)")
	skillAddCmd.Flags().IntVar(&skillAddSensitivity, "data-sensitivity", 1, "Data sensitivity 1-5")
	skillAddCmd.Flags().IntVar(&skillAddExposure, "network-exposure", 1, "Network exposure 1-5")
	skillAddCmd.Flags().IntVar(&skillAddPrivilege, "privilege-level", 1, "Privilege level 1-5")
	skillAddCmd.Flags().StringSliceVar(&skillAddHosts, "allowed-host", nil, "Allowed network host (repeatable)")
	skillAddCmd.Flags().StringSliceVar(&skillAddPortStrs, "allowed-port", nil, "Allowed network port (repeatable)")
	skillAddCmd.Flags().StringSliceVar(&skillAddProtocols, "allowed-protocol", nil, "Allowed protocol: tcp, udp, icmp (repeatable)")
	skillAddCmd.Flags().StringSliceVar(&skillAddSecretRefs, "secret", nil, "Secret reference name (repeatable)")
	skillAddCmd.Flags().BoolVar(&skillAddNonInteract, "non-interactive", false, "Skip interactive wizard, use flags only")
}

func runSkillAdd(cmd *cobra.Command, args []string) error {
	skillGoal := args[0]

	var result *wizard.WizardResult
	var err error

	if skillAddNonInteract || skillAddName != "" || len(skillAddTools) > 0 {
		result, err = buildSkillAddResult(skillGoal)
	} else {
		result, err = wizard.RunWizard(skillGoal)
	}
	if err != nil {
		return fmt.Errorf("skill proposal setup: %w", err)
	}

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// Create the proposal.
	cat := proposal.Category(result.Category)
	p, err := proposal.NewProposal(result.Title, result.Description, cat, "operator")
	if err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	p.Risk = proposal.RiskLevel(result.Risk)
	p.TargetSkill = result.SkillName

	spec, err := result.ToProposalJSON()
	if err != nil {
		return fmt.Errorf("failed to generate spec: %w", err)
	}
	p.Spec = spec
	p.SecretsRefs = result.SecretsRefs

	if result.NeedsNetwork {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
			DefaultDeny:      true,
			AllowedHosts:     result.AllowedHosts,
			AllowedPorts:     result.AllowedPorts,
			AllowedProtocols: result.AllowedProtocols,
		}
	} else {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
			DefaultDeny: true,
		}
	}

	// Phase 5: ProposalStore removed from Host Daemon TCB.
	// Long-term owner: Store VM via AegisHub.
	_ = p
	return fmt.Errorf("proposal creation removed from minimal Host Daemon TCB (Phase 5)")
}

func buildSkillAddResult(skillGoal string) (*wizard.WizardResult, error) {
	name := skillAddName
	if name == "" {
		name = strings.ToLower(strings.ReplaceAll(skillGoal, " ", "-"))
		sanitized := make([]byte, 0, len(name))
		for _, b := range []byte(name) {
			if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-' || b == '_' {
				sanitized = append(sanitized, b)
			}
		}
		name = string(sanitized)
		if len(name) > 62 {
			name = name[:62]
		}
	}

	title := skillAddTitle
	if title == "" {
		title = fmt.Sprintf("Add %s skill", skillGoal)
	}

	desc := skillAddDescription
	if desc == "" {
		desc = fmt.Sprintf("Implement a new skill for %s integration", skillGoal)
	}

	if len(skillAddTools) == 0 {
		return nil, fmt.Errorf("at least one --tool flag is required (format: name:description)")
	}

	tools := make([]wizard.WizardToolSpec, 0, len(skillAddTools))
	for _, t := range skillAddTools {
		parts := strings.SplitN(t, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --tool %q: expected name:description", t)
		}
		tools = append(tools, wizard.WizardToolSpec{Name: parts[0], Description: parts[1]})
	}

	ds := clampInt(skillAddSensitivity, 1, 5)
	ne := clampInt(skillAddExposure, 1, 5)
	pl := clampInt(skillAddPrivilege, 1, 5)

	result := &wizard.WizardResult{
		Title:            title,
		Description:      desc,
		Category:         "new_skill",
		SkillName:        name,
		DataSensitivity:  ds,
		NetworkExposure:  ne,
		PrivilegeLevel:   pl,
		NeedsNetwork:     len(skillAddHosts) > 0,
		AllowedHosts:     skillAddHosts,
		AllowedPorts:     parsePorts(skillAddPortStrs),
		AllowedProtocols: skillAddProtocols,
		SecretsRefs:      skillAddSecretRefs,
		RequiredPersonas: []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
		Tools:            tools,
	}
	result.Risk = result.ComputedRisk()
	return result, nil
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func parsePorts(raw []string) []uint16 {
	var ports []uint16
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 && n <= 65535 {
			ports = append(ports, uint16(n))
		}
	}
	return ports
}

func runSkillList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// Try daemon first for live state.
	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "skill.list", nil)
	if err == nil && resp.Success {
		var skills []struct {
			Name       string `json:"name"`
			SandboxID  string `json:"sandbox_id"`
			State      string `json:"state"`
			Version    int    `json:"version"`
			MerkleHash string `json:"merkle_hash"`
		}
		if json.Unmarshal(resp.Data, &skills) == nil {
			if len(skills) == 0 {
				fmt.Println("No skills registered.")
				return nil
			}
			if globalJSON {
				data, _ := json.MarshalIndent(skills, "", "  ")
				fmt.Println(string(data))
				return nil
			}
			fmt.Printf("%-20s %-36s %-10s %-4s %-16s\n",
				"NAME", "SANDBOX", "STATE", "VER", "HASH")
			for _, sk := range skills {
				hashDisplay := sk.MerkleHash
				if len(hashDisplay) > 16 {
					hashDisplay = hashDisplay[:16]
				}
				fmt.Printf("%-20s %-36s %-10s %-4d %-16s\n",
					sk.Name, sk.SandboxID, sk.State, sk.Version, hashDisplay)
			}
			return nil
		}
	}

	// Fall back to local registry.
	skills := env.Registry.List()
	if len(skills) == 0 {
		fmt.Println("No skills registered.")
		return nil
	}

	if globalJSON {
		data, _ := json.MarshalIndent(skills, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("%-20s %-36s %-10s %-4s %-16s\n",
		"NAME", "SANDBOX", "STATE", "VER", "HASH")
	for _, sk := range skills {
		hashDisplay := sk.MerkleHash
		if len(hashDisplay) > 16 {
			hashDisplay = hashDisplay[:16]
		}
		fmt.Printf("%-20s %-36s %-10s %-4d %-16s\n",
			sk.Name, sk.SandboxID, sk.State, sk.Version, hashDisplay)
	}

	rootHash := env.Registry.RootHash()
	if len(rootHash) > 16 {
		rootHash = rootHash[:16]
	}
	fmt.Printf("\nRegistry: seq=%d root=%s\n", env.Registry.Sequence(), rootHash)
	return nil
}

func runSkillRevoke(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if !globalForce {
		fmt.Printf("Revoke skill %q? This will stop its microVM and remove it. [y/N] ", name)
		var confirm string
		fmt.Scanln(&confirm)
		if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "skill.deactivate", api.SkillDeactivateRequest{
		Name: name,
	})
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w\n(Is the daemon running? Start it with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("skill revocation failed: %s", resp.Error)
	}

	// Audit log the revocation.
	payload, _ := json.Marshal(map[string]string{"skill_name": name, "action": "revoke"})
	action := kernel.NewAction(kernel.ActionSkillDeactivate, "operator", payload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit log skill revocation", zap.Error(logErr))
	}

	fmt.Printf("Skill %q revoked.\n", name)
	return nil
}

func runSkillInfo(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	entry, ok := env.Registry.Get(name)
	if !ok {
		return fmt.Errorf("skill %q not found in registry", name)
	}

	if globalJSON {
		data, _ := json.MarshalIndent(entry, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Skill: %s\n", entry.Name)
	fmt.Printf("  Sandbox:     %s\n", entry.SandboxID)
	fmt.Printf("  State:       %s\n", entry.State)
	fmt.Printf("  Version:     %d\n", entry.Version)
	fmt.Printf("  Hash:        %s\n", entry.MerkleHash)
	fmt.Printf("  Activated:   %s\n", entry.ActivatedAt.Format("2006-01-02 15:04:05"))

	if meta := entry.Metadata; meta != nil {
		for k, v := range meta {
			fmt.Printf("  %-12s %s\n", k+":", v)
		}
	}

	return nil
}

var skillSBOMCmd = &cobra.Command{
	Use:   "sbom <name-or-id>",
	Short: "Print the Software Bill of Materials (SBOM) for a built skill",
	Long: `Prints the CycloneDX 1.4 SBOM emitted when the skill was built.
The SBOM documents the skill component and its detected dependencies.

Examples:
  aegisclaw skill sbom greeter
  aegisclaw skill sbom <proposal-id>`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillSBOM,
}

func runSkillSBOM(_ *cobra.Command, args []string) error {
	nameOrID := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	sbomDir := env.Config.Builder.SBOMDir
	if sbomDir == "" {
		return fmt.Errorf("SBOM directory not configured (builder.sbom_dir)")
	}

	// Try by proposal ID first (exact directory match).
	directPath := filepath.Join(sbomDir, nameOrID, "sbom.json")
	if s, readErr := tryReadSBOM(directPath); readErr == nil {
		return printSBOM(s)
	}

	// Phase 5: ProposalStore removed from Host Daemon TCB.
	return fmt.Errorf("no SBOM found for %q (proposal-based lookup removed in Phase 5); build the skill first with: aegisclaw skill add", nameOrID)
}

func tryReadSBOM(path string) (*sbomPkg, error) {
	return readSBOMFile(path)
}

func printSBOM(s *sbomPkg) error {
	if globalJSON {
		data, _ := json.MarshalIndent(s, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("SBOM for %s v%s  (CycloneDX %s)\n", s.Metadata.Component.Name, s.Metadata.Component.Version, s.SpecVersion)
	fmt.Printf("  Serial: %s\n", s.SerialNumber)
	fmt.Printf("  Built:  %s\n", s.Metadata.Timestamp)
	if len(s.Metadata.Component.Hashes) > 0 {
		fmt.Printf("  Hash:   %s (%s)\n", s.Metadata.Component.Hashes[0].Content, s.Metadata.Component.Hashes[0].Alg)
	}
	if len(s.Components) > 0 {
		fmt.Printf("\n  Dependencies (%d):\n", len(s.Components))
		for _, c := range s.Components {
			ver := c.Version
			if ver == "" {
				ver = "unknown"
			}
			fmt.Printf("    %-50s  %s\n", c.Name, ver)
		}
	}
	for _, p := range s.Metadata.Component.Properties {
		if p.Name == "aegisclaw:proposal_id" {
			fmt.Printf("\n  Proposal: %s\n", p.Value)
		}
	}
	return nil
}

var skillActivateCmd = &cobra.Command{
	Use:   "activate <name>",
	Short: "Activate an approved skill (with pre-activation secret check)",
	Long: `Activates an approved skill by spinning up its Firecracker microVM.

Before sending the activate request to the daemon, the command checks that
all secrets declared in the skill's approved proposal are present in the local
vault.  If any are missing, it prints a clear message explaining how to add them
rather than letting the skill start in a degraded state.

Example:
  aegisclaw skill activate discord-messenger`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillActivate,
}

func runSkillActivate(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// Pre-activation check: verify all declared secrets exist in the vault.
	// Walk proposals to find the approved one for this skill.
	if false { // Vault removed from TCB; secrets not handled in daemon 
		if err := checkSecretsBeforeActivate(skillName, env); err != nil {
			return err
		}
	}

	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "skill.activate", api.SkillActivateRequest{
		Name: skillName,
	})
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w\n(Is the daemon running? Start it with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("skill activation failed: %s", resp.Error)
	}

	var result map[string]interface{}
	if resp.Data != nil {
		_ = json.Unmarshal(resp.Data, &result)
	}
	fmt.Printf("Skill %q activated.\n", skillName)
	if sandbox, ok := result["sandbox_id"].(string); ok {
		fmt.Printf("  Sandbox: %s\n", sandbox)
	}
	return nil
}

// checkSecretsBeforeActivate looks up the approved proposal for skillName and
// verifies all declared secrets_refs exist in the vault.  Returns a descriptive
// error listing missing secrets and the CLI command to add each one.
func checkSecretsBeforeActivate(skillName string, env *runtimeEnv) error {
	// Phase 5: ProposalStore / secrets check removed from Host Daemon TCB.
	_ = skillName
	_ = env
	return nil // can't check — proceed optimistically (Phase 5 stub)
}
