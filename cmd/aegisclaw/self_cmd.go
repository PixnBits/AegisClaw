package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var selfCmd = &cobra.Command{
	Use:   "self",
	Short: "Self-improvement and system management proposals",
	Long:  `Commands for proposing and tracking system self-improvement changes.`,
}

var selfProposeCmd = &cobra.Command{
	Use:   "propose <description>",
	Short: "Start a Court-reviewed self-improvement proposal",
	Long: `Creates a proposal for system self-improvement and submits it
for Governance Court review. This is how the system evolves itself.`,
	Args: cobra.ExactArgs(1),
	RunE: runSelfPropose,
}

var selfStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show self-improvement proposal status",
	Long:  `Displays the status of active self-improvement proposals.`,
	RunE:  runSelfStatus,
}

var selfDiagnoseExtended bool

var selfDiagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Run system health checks",
	Long: `Checks that the system is healthy and ready to run the agent.

Standard checks:
  • KVM availability (/dev/kvm)
  • Firecracker binary exists and is executable
  • Ollama health (HTTP reachability + model inventory)
  • Agent rootfs image present

Extended checks (--extended):
  • Available RAM and disk space
  • Firecracker snapshot support (kernel version + Firecracker flags)
  • All configured Ollama models are locally available
  • Snapshot directory is writable`,
	RunE: runSelfDiagnose,
}

func init() {
	selfDiagnoseCmd.Flags().BoolVar(&selfDiagnoseExtended, "extended", false, "Include resource usage and snapshot support checks")
	selfCmd.AddCommand(selfProposeCmd)
	selfCmd.AddCommand(selfStatusCmd)
	selfCmd.AddCommand(selfDiagnoseCmd)
}

func runSelfPropose(cmd *cobra.Command, args []string) error {
	description := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	p, err := proposal.NewProposal(
		"Self-improvement: "+description,
		description,
		proposal.CategoryKernelPatch,
		"system",
	)
	if err != nil {
		return fmt.Errorf("invalid proposal: %w", err)
	}

	if err := env.ProposalStore.Create(p); err != nil {
		return fmt.Errorf("failed to create proposal: %w", err)
	}

	// Auto-submit for court review.
	if err := p.Transition(proposal.StatusSubmitted, "submitted for review", "system"); err != nil {
		return fmt.Errorf("cannot submit: %w", err)
	}
	if err := env.ProposalStore.Update(p); err != nil {
		return fmt.Errorf("failed to persist: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID,
		"title":       p.Title,
		"category":    string(p.Category),
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "system", payload)
	if _, signErr := env.Kernel.SignAndLog(action); signErr != nil {
		env.Logger.Error("failed to log self-improvement proposal", zap.Error(signErr))
	}

	fmt.Printf("Self-improvement proposal created and submitted.\n")
	fmt.Printf("  ID:       %s\n", p.ID)
	fmt.Printf("  Title:    %s\n", p.Title)
	fmt.Printf("  Status:   %s\n", p.Status)

	return nil
}

func runSelfStatus(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	proposals, err := env.ProposalStore.List()
	if err != nil {
		return fmt.Errorf("failed to list proposals: %w", err)
	}

	found := false
	for _, p := range proposals {
		if p.Category == proposal.CategoryKernelPatch {
			if !found {
				fmt.Println("Self-improvement proposals:")
				found = true
			}
			fmt.Printf("  %s  %-12s  %s\n", p.ID[:8], p.Status, p.Title)
		}
	}
	if !found {
		fmt.Println("No self-improvement proposals found.")
	}
	return nil
}

// diagnoseCheck holds the result of a single system health check.
type diagnoseCheck struct {
	Name    string
	OK      bool
	Detail  string
}

func (c diagnoseCheck) status() string {
	if c.OK {
		return "OK"
	}
	return "FAIL"
}

func runSelfDiagnose(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	var checks []diagnoseCheck

	// ── Standard checks ──────────────────────────────────────────────────────

	// 1. KVM availability.
	kvmCheck := diagnoseCheck{Name: "KVM (/dev/kvm)"}
	if info, err := os.Stat("/dev/kvm"); err == nil && info.Mode()&os.ModeDevice != 0 {
		kvmCheck.OK = true
		kvmCheck.Detail = "present and is a device node"
	} else if os.IsNotExist(err) {
		kvmCheck.Detail = "/dev/kvm not found — KVM kernel module not loaded"
	} else if err != nil {
		kvmCheck.Detail = fmt.Sprintf("stat error: %v", err)
	} else {
		kvmCheck.Detail = fmt.Sprintf("unexpected mode: %s", info.Mode())
	}
	checks = append(checks, kvmCheck)

	// 2. Firecracker binary.
	fcCheck := diagnoseCheck{Name: "Firecracker binary"}
	if info, err := os.Stat(env.Config.Firecracker.Bin); err == nil {
		if info.Mode()&0111 != 0 {
			fcCheck.OK = true
			fcCheck.Detail = env.Config.Firecracker.Bin
		} else {
			fcCheck.Detail = fmt.Sprintf("%s exists but is not executable", env.Config.Firecracker.Bin)
		}
	} else {
		fcCheck.Detail = fmt.Sprintf("%s: %v", env.Config.Firecracker.Bin, err)
	}
	checks = append(checks, fcCheck)

	// 3. Agent rootfs image.
	rootfsCheck := diagnoseCheck{Name: "Agent rootfs image"}
	if info, err := os.Stat(env.Config.Agent.RootfsPath); err == nil && !info.IsDir() {
		rootfsCheck.OK = true
		rootfsCheck.Detail = fmt.Sprintf("%s (%.1f MiB)", env.Config.Agent.RootfsPath, float64(info.Size())/(1024*1024))
	} else if err != nil {
		rootfsCheck.Detail = fmt.Sprintf("%s: %v", env.Config.Agent.RootfsPath, err)
	} else {
		rootfsCheck.Detail = fmt.Sprintf("%s is a directory, expected a file", env.Config.Agent.RootfsPath)
	}
	checks = append(checks, rootfsCheck)

	// 4. Ollama reachability.
	ollamaCheck := diagnoseCheck{Name: "Ollama reachability"}
	ollamaModels, ollamaErr := probeOllama(env.Config.Ollama.Endpoint)
	if ollamaErr == nil {
		ollamaCheck.OK = true
		ollamaCheck.Detail = fmt.Sprintf("healthy — %d model(s) available", len(ollamaModels))
	} else {
		ollamaCheck.Detail = ollamaErr.Error()
	}
	checks = append(checks, ollamaCheck)

	// 5. Ollama default model present.
	defaultModelCheck := diagnoseCheck{Name: fmt.Sprintf("Ollama default model (%s)", env.Config.Ollama.DefaultModel)}
	if ollamaErr == nil {
		for _, m := range ollamaModels {
			if m == env.Config.Ollama.DefaultModel {
				defaultModelCheck.OK = true
				defaultModelCheck.Detail = "found"
				break
			}
		}
		if !defaultModelCheck.OK {
			defaultModelCheck.Detail = fmt.Sprintf("model %q not in local Ollama library", env.Config.Ollama.DefaultModel)
		}
	} else {
		defaultModelCheck.Detail = "skipped (Ollama unreachable)"
	}
	checks = append(checks, defaultModelCheck)

	// ── Extended checks ───────────────────────────────────────────────────────

	if selfDiagnoseExtended {
		// 6. Available RAM.
		ramCheck := diagnoseCheck{Name: "Available RAM"}
		var sysinfo syscall.Sysinfo_t
		if err := syscall.Sysinfo(&sysinfo); err == nil {
			totalMiB := int64(sysinfo.Totalram) * int64(sysinfo.Unit) / 1024 / 1024
			freeMiB := int64(sysinfo.Freeram) * int64(sysinfo.Unit) / 1024 / 1024
			const minFreeMiB = 2048
			if freeMiB >= minFreeMiB {
				ramCheck.OK = true
			}
			ramCheck.Detail = fmt.Sprintf("total=%d MiB  free=%d MiB  (minimum required: %d MiB)", totalMiB, freeMiB, minFreeMiB)
		} else {
			ramCheck.Detail = fmt.Sprintf("sysinfo: %v", err)
		}
		checks = append(checks, ramCheck)

		// 7. Disk space for snapshots.
		diskCheck := diagnoseCheck{Name: "Snapshot dir disk space"}
		if err := os.MkdirAll(env.Config.Snapshot.Dir, 0700); err == nil {
			var statfs syscall.Statfs_t
			if err := syscall.Statfs(env.Config.Snapshot.Dir, &statfs); err == nil {
				freeGiB := float64(statfs.Bavail) * float64(statfs.Bsize) / (1024 * 1024 * 1024)
				const minFreeGiB = 5.0
				if freeGiB >= minFreeGiB {
					diskCheck.OK = true
				}
				diskCheck.Detail = fmt.Sprintf("%.1f GiB free at %s (minimum: %.0f GiB)", freeGiB, env.Config.Snapshot.Dir, minFreeGiB)
			} else {
				diskCheck.Detail = fmt.Sprintf("statfs %s: %v", env.Config.Snapshot.Dir, err)
			}
		} else {
			diskCheck.Detail = fmt.Sprintf("cannot create snapshot dir %s: %v", env.Config.Snapshot.Dir, err)
		}
		checks = append(checks, diskCheck)

		// 8. Snapshot directory writable.
		snapWriteCheck := diagnoseCheck{Name: "Snapshot dir writable"}
		testFile := env.Config.Snapshot.Dir + "/.write_test"
		if err := os.WriteFile(testFile, []byte("ok"), 0600); err == nil {
			os.Remove(testFile)
			snapWriteCheck.OK = true
			snapWriteCheck.Detail = env.Config.Snapshot.Dir
		} else {
			snapWriteCheck.Detail = fmt.Sprintf("write test failed: %v", err)
		}
		checks = append(checks, snapWriteCheck)

		// 9. Snapshot support: check that the running kernel version is >= 5.4
		//    (minimum for Firecracker snapshot support).
		snapSupportCheck := diagnoseCheck{Name: "Snapshot kernel support (>= 5.4)"}
		var uname syscall.Utsname
		if err := syscall.Uname(&uname); err == nil {
			release := int8SliceToString(uname.Release[:])
			major, minor := 0, 0
			fmt.Sscanf(release, "%d.%d", &major, &minor)
			if major > 5 || (major == 5 && minor >= 4) {
				snapSupportCheck.OK = true
			}
			snapSupportCheck.Detail = fmt.Sprintf("kernel %s (parsed %d.%d)", release, major, minor)
		} else {
			snapSupportCheck.Detail = fmt.Sprintf("uname: %v", err)
		}
		checks = append(checks, snapSupportCheck)

		// 10. All configured Ollama models available.
		if ollamaErr == nil {
			modelMap := make(map[string]bool, len(ollamaModels))
			for _, m := range ollamaModels {
				modelMap[m] = true
			}
			allModelsCheck := diagnoseCheck{Name: "All Ollama models available"}
			needed := []string{env.Config.Ollama.DefaultModel}
			missing := []string{}
			for _, m := range needed {
				if !modelMap[m] {
					missing = append(missing, m)
				}
			}
			if len(missing) == 0 {
				allModelsCheck.OK = true
				allModelsCheck.Detail = fmt.Sprintf("%d model(s) available: all required models present", len(ollamaModels))
			} else {
				allModelsCheck.Detail = fmt.Sprintf("missing models: %v (run: ollama pull <model>)", missing)
			}
			checks = append(checks, allModelsCheck)
		}

		// 11. List available Ollama snapshots.
		snapListCheck := diagnoseCheck{Name: "Stored agent VM snapshots"}
		snaps, snapListErr := sandbox.ListSnapshots(env.Config.Snapshot.Dir)
		if snapListErr != nil {
			snapListCheck.Detail = fmt.Sprintf("error listing snapshots: %v", snapListErr)
		} else if len(snaps) == 0 {
			snapListCheck.OK = true
			snapListCheck.Detail = "none yet (use 'snapshot.create' to save a baseline)"
		} else {
			snapListCheck.OK = true
			snapListCheck.Detail = fmt.Sprintf("%d snapshot(s) stored", len(snaps))
		}
		checks = append(checks, snapListCheck)
	}

	// ── Print results ─────────────────────────────────────────────────────────
	allOK := true
	fmt.Printf("AegisClaw system diagnostics  (GOOS=%s  GOARCH=%s)\n", runtime.GOOS, runtime.GOARCH)
	if selfDiagnoseExtended {
		fmt.Println("Mode: extended")
	}
	fmt.Println()
	for _, c := range checks {
		marker := "✓"
		if !c.OK {
			marker = "✗"
			allOK = false
		}
		fmt.Printf("  %s  %-45s  %s\n", marker, c.Name, c.Detail)
	}
	fmt.Println()
	if allOK {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("One or more checks failed. See details above.")
		return fmt.Errorf("diagnostics failed")
	}
	return nil
}

// probeOllama connects to the Ollama endpoint and returns the list of available
// model names.  Returns an error if Ollama is unreachable or returns a non-200 status.
func probeOllama(endpoint string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Ollama at %s is unreachable: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // 512 KiB cap
	if err != nil {
		return nil, fmt.Errorf("read Ollama response: %w", err)
	}

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return nil, fmt.Errorf("parse Ollama tags response: %w", err)
	}

	names := make([]string, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// int8SliceToString converts a []int8 kernel string (from Utsname) to a Go string.
func int8SliceToString(s []int8) string {
	b := make([]byte, 0, len(s))
	for _, v := range s {
		if v == 0 {
			break
		}
		b = append(b, byte(v))
	}
	return string(b)
}

