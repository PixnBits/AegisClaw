package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/vault"
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

	if err := env.ProposalStore.Create(p); err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	// Auto-submit for court review.
	if err := p.Transition(proposal.StatusSubmitted, "submitted for review", "operator"); err != nil {
		return fmt.Errorf("cannot submit: %w", err)
	}
	if err := env.ProposalStore.Update(p); err != nil {
		return fmt.Errorf("failed to persist: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID,
		"title":       p.Title,
		"category":    string(p.Category),
		"skill_name":  result.SkillName,
		"risk":        result.Risk,
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "operator", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log proposal creation", zap.Error(signErr))
	}

	if globalJSON {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"proposal_id": p.ID,
			"title":       p.Title,
			"skill":       p.TargetSkill,
			"status":      string(p.Status),
			"risk":        string(p.Risk),
		}, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Println()
		fmt.Printf("Skill proposal created and submitted for review.\n")
		fmt.Printf("  ID:       %s\n", p.ID)
		fmt.Printf("  Title:    %s\n", p.Title)
		fmt.Printf("  Skill:    %s\n", p.TargetSkill)
		fmt.Printf("  Risk:     %s\n", p.Risk)
		fmt.Printf("  Status:   %s\n", p.Status)

		if len(p.SecretsRefs) > 0 {
			fmt.Printf("  Secrets:  %v\n", p.SecretsRefs)
		}
	}

	// Send the proposal to the daemon for court review. The daemon runs as
	// root with Firecracker access and owns the reviewer sandboxes.
	proposalData, err := p.Marshal()
	if err != nil {
		return fmt.Errorf("failed to serialize proposal: %w", err)
	}
	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "court.review", api.CourtReviewRequest{
		ProposalID:   p.ID,
		ProposalData: proposalData,
	})
	if err != nil {
		fmt.Printf("\n  Court review could not be started: %v\n", err)
		fmt.Printf("  (Is the daemon running? Start it with: sudo aegisclaw start)\n")
		return nil
	}
	if resp.Error != "" {
		fmt.Printf("\n  Court review failed: %s\n", resp.Error)
		return nil
	}
	fmt.Printf("\n  Court review initiated.\n")

	return nil
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

	// Try by skill name: scan all proposals.
	proposals, listErr := env.ProposalStore.List()
	if listErr != nil {
		return fmt.Errorf("list proposals: %w", listErr)
	}
	for _, p := range proposals {
		path := filepath.Join(sbomDir, p.ID, "sbom.json")
		s, readErr := tryReadSBOM(path)
		if readErr != nil {
			continue
		}
		if s.Metadata.Component.Name == nameOrID {
			return printSBOM(s)
		}
	}
	return fmt.Errorf("no SBOM found for %q — build the skill first with: aegisclaw skill add", nameOrID)
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
	if env.Vault != nil {
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
	summaries, err := env.ProposalStore.List()
	if err != nil {
		return nil // can't check — proceed optimistically
	}

	var secretsRefs []string
	for _, s := range summaries {
		full, err := env.ProposalStore.Get(s.ID)
		if err != nil || full == nil {
			continue
		}
		if full.TargetSkill != skillName {
			continue
		}
		if full.Status != proposal.StatusApproved && full.Status != proposal.StatusImplementing && full.Status != proposal.StatusComplete {
			continue
		}
		secretsRefs = full.SecretsRefs
		break
	}

	if len(secretsRefs) == 0 {
		return nil // no secrets required
	}

	// Use the already-opened vault if available; otherwise open it.
	v := env.Vault
	if v == nil {
		if env.Kernel == nil {
			return nil // can't check without kernel key — proceed optimistically
		}
		var vaultErr error
		v, vaultErr = vault.NewVault(env.Config.Vault.Dir, env.Kernel.PrivateKeyBytes(), env.Logger)
		if vaultErr != nil {
			return nil // can't check vault — proceed and let daemon handle it
		}
	}

	var missing []string
	for _, ref := range secretsRefs {
		if !v.Has(ref) {
			missing = append(missing, ref)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Missing required secret(s) for skill %q:\n", skillName))
	for _, m := range missing {
		b.WriteString(fmt.Sprintf("  - %s\n", m))
		b.WriteString(fmt.Sprintf("    Add with: aegisclaw secrets add %s --skill %s\n", m, skillName))
	}
	return fmt.Errorf("%s", b.String())
}
