package main

// review_bootstrap_test.go tests the built-in periodic security review skill
// registration and helper functions.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap/zaptest"
)

// makeTestEnvForReview returns a minimal runtimeEnv for testing the review
// bootstrap.  The Runtime field is intentionally nil to prevent sandbox
// creation in unit tests.
func makeTestEnvForReview(t *testing.T) *runtimeEnv {
	t.Helper()
	kernel.ResetInstance()
	t.Cleanup(func() { kernel.ResetInstance() })

	logger := zaptest.NewLogger(t)
	kern, err := kernel.GetInstance(logger, t.TempDir())
	if err != nil {
		t.Fatalf("kernel.GetInstance: %v", err)
	}
	t.Cleanup(func() { kern.Shutdown() })

	reg, err := sandbox.NewSkillRegistry(t.TempDir() + "/registry.json")
	if err != nil {
		t.Fatalf("NewSkillRegistry: %v", err)
	}

	bus, err := eventbus.New(eventbus.Config{
		Dir:              t.TempDir(),
		MaxPendingTimers: 50,
	})
	if err != nil {
		t.Fatalf("eventbus.New: %v", err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	mem, err := memory.NewStore(memory.StoreConfig{Dir: t.TempDir()}, identity)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Review.Enabled = true

	return &runtimeEnv{
		Logger:      logger,
		Config:      &cfg,
		Kernel:      kern,
		Registry:    reg,
		EventBus:    bus,
		MemoryStore: mem,
		// Runtime intentionally nil — no sandbox spawning in unit tests.
	}
}

// TestEnsureReviewSkillsRegistered_AllFourSkillsAppearInList verifies that all
// four built-in review skills appear in the skill registry after bootstrap.
func TestEnsureReviewSkillsRegistered_AllFourSkillsAppearInList(t *testing.T) {
	env := makeTestEnvForReview(t)

	ensureReviewSkillsRegistered(t.Context(), env)

	skills := env.Registry.List()
	nameSet := make(map[string]bool, len(skills))
	for _, s := range skills {
		nameSet[s.Name] = true
	}

	for _, name := range []string{
		reviewSkillSecurityAuditor,
		reviewSkillAccessReviewer,
		reviewSkillSecretsVerifier,
		reviewSkillPolicyRefresher,
	} {
		if !nameSet[name] {
			t.Errorf("expected skill %q to appear in registry list, but it was absent", name)
		}
	}
}

// TestEnsureReviewSkillsRegistered_TimersCreated verifies that each review skill
// gets an active cron timer in the event bus.
func TestEnsureReviewSkillsRegistered_TimersCreated(t *testing.T) {
	env := makeTestEnvForReview(t)

	ensureReviewSkillsRegistered(t.Context(), env)

	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	timerNames := make(map[string]bool, len(timers))
	for _, ti := range timers {
		if ti.Owner == reviewTimerOwner {
			timerNames[ti.Name] = true
		}
	}

	for _, name := range []string{
		reviewSkillSecurityAuditor,
		reviewSkillAccessReviewer,
		reviewSkillSecretsVerifier,
		reviewSkillPolicyRefresher,
	} {
		if !timerNames[name] {
			t.Errorf("expected active timer for review skill %q, but none found", name)
		}
	}
}

// TestEnsureReviewSkillsRegistered_IdempotentTimers verifies that calling
// ensureReviewSkillsRegistered twice does not create duplicate timers.
func TestEnsureReviewSkillsRegistered_IdempotentTimers(t *testing.T) {
	env := makeTestEnvForReview(t)

	ensureReviewSkillsRegistered(t.Context(), env)
	ensureReviewSkillsRegistered(t.Context(), env)

	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	counts := make(map[string]int)
	for _, ti := range timers {
		if ti.Owner == reviewTimerOwner {
			counts[ti.Name]++
		}
	}

	for _, name := range []string{
		reviewSkillSecurityAuditor,
		reviewSkillAccessReviewer,
		reviewSkillSecretsVerifier,
		reviewSkillPolicyRefresher,
	} {
		if counts[name] != 1 {
			t.Errorf("expected exactly 1 timer for %q, got %d", name, counts[name])
		}
	}
}

// TestEnsureReviewSkillsRegistered_DisabledConfig verifies that when
// config.Review.Enabled=false no timers are created.
func TestEnsureReviewSkillsRegistered_DisabledConfig(t *testing.T) {
	env := makeTestEnvForReview(t)
	env.Config.Review.Enabled = false

	ensureReviewSkillsRegistered(t.Context(), env)

	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	for _, ti := range timers {
		if ti.Owner == reviewTimerOwner {
			t.Errorf("expected no review timers when disabled, but found timer %q", ti.Name)
		}
	}
}

// TestEnsureReviewSkillsRegistered_TimerPayloadHasTaskDescription verifies that
// each timer's payload contains a non-empty task_description so that
// dispatchTimerWakeup will spawn a worker on fire.
func TestEnsureReviewSkillsRegistered_TimerPayloadHasTaskDescription(t *testing.T) {
	env := makeTestEnvForReview(t)

	ensureReviewSkillsRegistered(t.Context(), env)

	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	for _, ti := range timers {
		if ti.Owner != reviewTimerOwner {
			continue
		}
		if len(ti.Payload) == 0 {
			t.Errorf("timer %q has empty payload", ti.Name)
			continue
		}
		var sp timerSpawnPayload
		if err := json.Unmarshal(ti.Payload, &sp); err != nil {
			t.Errorf("timer %q payload is not valid JSON: %v", ti.Name, err)
			continue
		}
		if sp.TaskDescription == "" {
			t.Errorf("timer %q payload has empty task_description", ti.Name)
		}
		if sp.Role == "" {
			t.Errorf("timer %q payload has empty role", ti.Name)
		}
	}
}

// TestEnsureReviewSkillsRegistered_BuiltinSandboxID verifies that all built-in
// review skills use the "builtin" synthetic sandbox ID in the registry.
func TestEnsureReviewSkillsRegistered_BuiltinSandboxID(t *testing.T) {
	env := makeTestEnvForReview(t)

	ensureReviewSkillsRegistered(t.Context(), env)

	for _, name := range []string{
		reviewSkillSecurityAuditor,
		reviewSkillAccessReviewer,
		reviewSkillSecretsVerifier,
		reviewSkillPolicyRefresher,
	} {
		entry, ok := env.Registry.Get(name)
		if !ok {
			t.Errorf("skill %q not found in registry", name)
			continue
		}
		if entry.SandboxID != "builtin" {
			t.Errorf("skill %q: expected SandboxID=builtin, got %q", name, entry.SandboxID)
		}
		if entry.State != sandbox.SkillStateInactive {
			t.Errorf("skill %q: expected state=inactive, got %q", name, entry.State)
		}
	}
}

// TestDisableReviewSkill verifies that disabling a review skill marks it
// disabled in the registry metadata and cancels its timer.
func TestDisableReviewSkill(t *testing.T) {
	env := makeTestEnvForReview(t)
	ensureReviewSkillsRegistered(t.Context(), env)

	// Verify timer exists before disable.
	before := env.EventBus.ListTimers(eventbus.TimerActive)
	found := false
	for _, ti := range before {
		if ti.Name == reviewSkillSecurityAuditor && ti.Owner == reviewTimerOwner {
			found = true
		}
	}
	if !found {
		t.Fatal("expected active timer before disable")
	}

	if err := disableReviewSkill(env, reviewSkillSecurityAuditor); err != nil {
		t.Fatalf("disableReviewSkill: %v", err)
	}

	// Timer should be cancelled.
	after := env.EventBus.ListTimers(eventbus.TimerActive)
	for _, ti := range after {
		if ti.Name == reviewSkillSecurityAuditor && ti.Owner == reviewTimerOwner {
			t.Error("expected timer to be cancelled after disable, but it is still active")
		}
	}

	// Metadata should have disabled=true.
	entry, ok := env.Registry.Get(reviewSkillSecurityAuditor)
	if !ok {
		t.Fatal("skill not found in registry after disable")
	}
	if entry.Metadata[reviewMetadataKeyDisabled] != "true" {
		t.Errorf("expected disabled=true in metadata, got %q", entry.Metadata[reviewMetadataKeyDisabled])
	}
}

// TestEnsureReviewSkillsRegistered_DisabledSkillSkipsTimer verifies that if a
// skill is already in the registry with disabled=true, no timer is created for
// it on re-bootstrap.
func TestEnsureReviewSkillsRegistered_DisabledSkillSkipsTimer(t *testing.T) {
	env := makeTestEnvForReview(t)

	// Pre-register the security auditor as disabled.
	_, err := env.Registry.RegisterBuiltIn(reviewSkillSecurityAuditor, map[string]string{
		reviewMetadataKeyType:    reviewMetadataValueBuiltIn,
		reviewMetadataKeyDisabled: "true",
	})
	if err != nil {
		t.Fatalf("RegisterBuiltIn: %v", err)
	}

	ensureReviewSkillsRegistered(t.Context(), env)

	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	for _, ti := range timers {
		if ti.Name == reviewSkillSecurityAuditor && ti.Owner == reviewTimerOwner {
			t.Error("expected no timer for disabled skill, but found one")
		}
	}
}

// TestBuildReviewSkillDefs_CustomCadence verifies that custom cadence
// expressions from config are used in the skill definitions.
func TestBuildReviewSkillDefs_CustomCadence(t *testing.T) {
	env := makeTestEnvForReview(t)
	env.Config.Review.SecurityAuditorCron = "@hourly"
	env.Config.Review.AccessReviewerCron = "@weekly"

	defs := buildReviewSkillDefs(env)

	defsMap := make(map[string]reviewSkillDef, len(defs))
	for _, d := range defs {
		defsMap[d.Name] = d
	}

	if defsMap[reviewSkillSecurityAuditor].Cron != "@hourly" {
		t.Errorf("expected @hourly cron for security-auditor, got %q", defsMap[reviewSkillSecurityAuditor].Cron)
	}
	if defsMap[reviewSkillAccessReviewer].Cron != "@weekly" {
		t.Errorf("expected @weekly cron for access-reviewer, got %q", defsMap[reviewSkillAccessReviewer].Cron)
	}
}

// TestRegistryUpdateMetadata verifies that UpdateMetadata persists changes.
func TestRegistryUpdateMetadata(t *testing.T) {
	reg, err := sandbox.NewSkillRegistry(t.TempDir() + "/registry.json")
	if err != nil {
		t.Fatalf("NewSkillRegistry: %v", err)
	}

	_, err = reg.RegisterBuiltIn("test-skill", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("RegisterBuiltIn: %v", err)
	}

	newMeta := map[string]string{"key": "value", "disabled": "true"}
	if err := reg.UpdateMetadata("test-skill", newMeta); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	entry, ok := reg.Get("test-skill")
	if !ok {
		t.Fatal("skill not found after UpdateMetadata")
	}
	if entry.Metadata["disabled"] != "true" {
		t.Errorf("expected disabled=true, got %q", entry.Metadata["disabled"])
	}
}

// TestNextCronTime_Quarterly verifies the @quarterly shorthand.
func TestNextCronTime_Quarterly(t *testing.T) {
	cases := []struct {
		from     time.Time
		wantYear int
		wantMon  time.Month
	}{
		// Mid-January → first day of April (Q2)
		{time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC), 2026, time.April},
		// Last day of March → first day of April (Q2)
		{time.Date(2026, time.March, 31, 23, 59, 0, 0, time.UTC), 2026, time.April},
		// Mid-June → first day of July (Q3)
		{time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC), 2026, time.July},
		// Mid-September → first day of October (Q4)
		{time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC), 2026, time.October},
		// Mid-November → first day of January (Q1 next year)
		{time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC), 2027, time.January},
		// December 31 → first day of January next year
		{time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC), 2027, time.January},
	}
	for _, tc := range cases {
		got := eventbus.NextCronTime("@quarterly", tc.from)
		if got.Year() != tc.wantYear || got.Month() != tc.wantMon || got.Day() != 1 {
			t.Errorf("from=%s: got %s, want %04d-%02d-01",
				tc.from.Format("2006-01-02"), got.Format("2006-01-02"), tc.wantYear, tc.wantMon)
		}
	}
}

// TestReviewSkillNamesAreValid verifies skill names only contain
// lowercase letters, digits, and hyphens (registry convention).
func TestReviewSkillNamesAreValid(t *testing.T) {
	for _, name := range []string{
		reviewSkillSecurityAuditor,
		reviewSkillAccessReviewer,
		reviewSkillSecretsVerifier,
		reviewSkillPolicyRefresher,
	} {
		for _, ch := range name {
			if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
				t.Errorf("skill name %q contains invalid character %q", name, ch)
			}
		}
		if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
			t.Errorf("skill name %q must not start or end with '-'", name)
		}
	}
}
