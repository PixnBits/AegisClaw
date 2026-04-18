package main

// review_bootstrap.go registers the four built-in periodic security review
// skills and their recurring timers on daemon startup.
//
// Security design notes:
//   - Each review skill is a timer-driven ephemeral worker, not a persistent
//     agent. This preserves the clean-slate execution guarantee: every review
//     starts from a fresh micro-VM with no state leakage between runs.
//   - All review runs write signed entries to the immutable Merkle audit log so
//     every action is verifiable and tamper-evident.
//   - Disabling any review is itself logged to the audit trail.
//   - Changes to this file must pass Governance Court review (CISO persona) via
//     the standard `aegisclaw skill add` / proposal pipeline before being merged.

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// Built-in review skill names — these are the canonical identifiers that
// appear in `aegisclaw skills list` and are used as timer names.
const (
	reviewSkillSecurityAuditor    = "security-auditor"
	reviewSkillAccessReviewer     = "access-permission-reviewer"
	reviewSkillSecretsVerifier    = "secrets-rotation-verifier"
	reviewSkillPolicyRefresher    = "policy-threat-model-refresher"
	reviewTimerOwner              = "system:review"
	reviewMetadataKeyType         = "type"
	reviewMetadataKeyDescription  = "description"
	reviewMetadataKeyCadence      = "cadence"
	reviewMetadataValueBuiltIn    = "built-in-review"
	reviewMetadataKeyDisabled     = "disabled"
)

// reviewSkillDef describes one built-in review skill.
type reviewSkillDef struct {
	// Name is the canonical skill / timer identifier.
	Name string
	// Description is a human-readable summary shown in skill list metadata.
	Description string
	// Cron is the schedule for this review (from config.Review.*Cron).
	Cron string
	// TaskDescription is sent as the worker task_description when the timer
	// fires, driving the ephemeral worker VM's behaviour.
	TaskDescription string
	// Role is the suggested worker persona for the review.
	Role string
}

// buildReviewSkillDefs constructs the canonical set of review skill definitions
// using the cadence expressions from config.  Called once during daemon init.
func buildReviewSkillDefs(env *runtimeEnv) []reviewSkillDef {
	cfg := env.Config.Review
	return []reviewSkillDef{
		{
			Name: reviewSkillSecurityAuditor,
			Description: "Core security auditor: queries audit log deltas since last run via " +
				"Merkle-tree, runs Governance Court (CISO lead) on changes, produces a " +
				"concise risk report, and notifies only on findings.",
			Cron: cfg.SecurityAuditorCron,
			TaskDescription: "Perform a periodic security audit. " +
				"1) Use list_audit_log to read the Merkle-tree audit entries since the last review run. " +
				"2) Evaluate each change for security risks; focus on skill activations, permission changes, secret access, and anomalous patterns. " +
				"3) For any flagged findings, record a risk assessment with evidence and recommended action. " +
				"4) Write a concise, signed risk report to the audit log using store_memory with key 'review.security-auditor.<timestamp>'. " +
				"5) Emit a notification only if findings exist. " +
				"6) Log the review completion with a cryptographic timestamp.",
			Role: "CISO",
		},
		{
			Name: reviewSkillAccessReviewer,
			Description: "Access and permission reviewer: scans all deployed skills, network " +
				"rules, and file permissions. Flags stale approvals, overly broad " +
				"permissions, or orphaned access. Generates a compliance scorecard.",
			Cron: cfg.AccessReviewerCron,
			TaskDescription: "Perform a periodic access and permission review. " +
				"1) Use list_skills to enumerate all deployed skills and inspect their metadata. " +
				"2) Review each skill's network policy, allowed hosts, and declared capabilities. " +
				"3) Flag skills with overly broad network access (e.g. no allowlist, wildcard hosts) or stale approval dates. " +
				"4) Check for orphaned or long-inactive skills that should be revoked. " +
				"5) For any questionable items, record them for Governance Court review via the proposal system. " +
				"6) Generate a compliance scorecard (pass/warn/fail per skill) and store it with key 'review.access-permission-reviewer.<timestamp>'. " +
				"7) Log the review completion with a cryptographic timestamp.",
			Role: "SecurityArchitect",
		},
		{
			Name: reviewSkillSecretsVerifier,
			Description: "Secrets rotation verifier: validates that all managed secrets have " +
				"been rotated within policy windows, checks for hardcoded credentials, " +
				"and escalates violations through the CISO persona.",
			Cron: cfg.SecretsVerifierCron,
			TaskDescription: "Perform a periodic secrets rotation verification. " +
				"1) Use list_skills to enumerate active skills and cross-reference their declared secrets_refs. " +
				"2) For each secret reference, verify the last rotation timestamp is within the policy window (default: 90 days). " +
				"3) Scan recent audit log entries for patterns that may indicate hardcoded credentials or unmanaged secrets. " +
				"4) For any secret past its rotation window, log an escalation entry with the secret reference name and overdue duration. " +
				"5) Store the rotation status report with key 'review.secrets-rotation-verifier.<timestamp>' and include cryptographic proof (entry hash). " +
				"6) If violations exist, record a high-priority finding for CISO review.",
			Role: "CISO",
		},
		{
			Name: reviewSkillPolicyRefresher,
			Description: "Policy and threat model refresher: reviews current governance rules " +
				"against latest risk patterns, evaluates whether existing policies match the " +
				"threat landscape, and suggests policy updates for human approval.",
			Cron: cfg.PolicyRefresherCron,
			TaskDescription: "Perform a periodic policy and threat model refresh. " +
				"1) Review the current governance rules and court persona weights by examining config and recent audit log entries. " +
				"2) Evaluate whether existing policies still match the current threat landscape based on recent security events, skill activations, and risk scores. " +
				"3) As the Security Architect persona, identify any policy gaps, outdated rules, or emerging threat vectors not currently covered. " +
				"4) Draft a list of suggested policy updates for human review and approval; do NOT apply changes autonomously. " +
				"5) Store the refresh report with key 'review.policy-threat-model-refresher.<timestamp>' including the suggested updates and their justifications. " +
				"6) Document this refresh run in the immutable audit log with a signed entry.",
			Role: "SecurityArchitect",
		},
	}
}

// ensureReviewSkillsRegistered registers all four built-in review skills in
// the skill registry and ensures each has an active cron timer in the event
// bus.  It is idempotent: existing registry entries and timers are left
// unchanged so operator edits survive daemon restarts.
//
// When config.Review.Enabled is false the function logs the disabled state to
// the immutable audit trail and returns without registering any timers.
func ensureReviewSkillsRegistered(ctx context.Context, env *runtimeEnv) {
	if env == nil || env.Registry == nil || env.EventBus == nil {
		return
	}

	if env.Config != nil && !env.Config.Review.Enabled {
		env.Logger.Warn("built-in review skills are disabled via config; skipping timer registration",
			zap.String("config_key", "review.enabled"),
		)
		// Log the disabled state to the immutable audit trail.
		disabledPayload, _ := json.Marshal(map[string]interface{}{
			"action":     "review_skills_disabled",
			"config_key": "review.enabled",
			"reason":     "config.Review.Enabled=false",
		})
		act := kernel.NewAction(kernel.ActionSkillDeactivate, "system:review", disabledPayload)
		if _, err := env.Kernel.SignAndLog(act); err != nil {
			env.Logger.Error("failed to audit-log review skills disabled state", zap.Error(err))
		}
		return
	}

	defs := buildReviewSkillDefs(env)
	for _, d := range defs {
		registerOneReviewSkill(ctx, env, d)
	}
}

// registerOneReviewSkill registers a single review skill and ensures its cron
// timer exists.  ctx is accepted for forward compatibility (e.g., cancellation
// support for future async registration steps).
func registerOneReviewSkill(_ context.Context, env *runtimeEnv, d reviewSkillDef) {
	// 1. Register in the skill registry (no-op if already present).
	meta := map[string]string{
		reviewMetadataKeyType:        reviewMetadataValueBuiltIn,
		reviewMetadataKeyDescription: d.Description,
		reviewMetadataKeyCadence:     d.Cron,
	}
	entry, err := env.Registry.RegisterBuiltIn(d.Name, meta)
	if err != nil {
		env.Logger.Error("review bootstrap: failed to register skill",
			zap.String("skill", d.Name),
			zap.Error(err),
		)
		return
	}

	// 2. Check whether the skill is marked as disabled in its metadata.
	if entry.Metadata[reviewMetadataKeyDisabled] == "true" {
		env.Logger.Info("review bootstrap: skill is disabled, skipping timer registration",
			zap.String("skill", d.Name),
		)
		return
	}

	// 3. Ensure there is exactly one active cron timer for this skill.
	//    If a timer with this name already exists (from a previous daemon run),
	//    we leave it in place so the cadence persists across restarts.
	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	for _, t := range timers {
		if t.Name == d.Name && t.Owner == reviewTimerOwner {
			env.Logger.Debug("review bootstrap: timer already registered",
				zap.String("skill", d.Name),
				zap.String("timer_id", t.TimerID),
				zap.String("cron", t.Cron),
			)
			return
		}
	}

	// 4. Register a new cron timer whose payload carries the task_description
	//    that dispatchTimerWakeup will pass to an ephemeral worker on each fire.
	spawnPayload := timerSpawnPayload{
		TaskDescription: d.TaskDescription,
		Role:            d.Role,
		TimeoutMins:     30,
		ToolsGranted:    []string{"list_audit_log", "list_skills", "store_memory", "retrieve_memory"},
	}
	rawPayload, err := json.Marshal(spawnPayload)
	if err != nil {
		env.Logger.Error("review bootstrap: failed to marshal timer payload",
			zap.String("skill", d.Name),
			zap.Error(err),
		)
		return
	}

	timer, err := env.EventBus.SetTimer(eventbus.SetTimerParams{
		Name:    d.Name,
		Type:    eventbus.TimerCron,
		Cron:    d.Cron,
		Payload: rawPayload,
		TaskID:  "review:" + d.Name,
		Owner:   reviewTimerOwner,
	})
	if err != nil {
		env.Logger.Error("review bootstrap: failed to register cron timer",
			zap.String("skill", d.Name),
			zap.String("cron", d.Cron),
			zap.Error(err),
		)
		return
	}

	env.Logger.Info("review bootstrap: registered built-in review skill",
		zap.String("skill", d.Name),
		zap.String("timer_id", timer.TimerID),
		zap.String("cron", d.Cron),
	)

	// 5. Audit-log the registration so it appears in the immutable chain.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"skill_name": d.Name,
		"timer_id":   timer.TimerID,
		"cron":       d.Cron,
		"action":     "review_skill_registered",
	})
	act := kernel.NewAction(kernel.ActionSkillActivate, "system:review", auditPayload)
	if _, logErr := env.Kernel.SignAndLog(act); logErr != nil {
		env.Logger.Error("review bootstrap: failed to audit-log skill registration",
			zap.String("skill", d.Name),
			zap.Error(logErr),
		)
	}
}

// disableReviewSkill marks a built-in review skill as disabled in the registry
// and cancels its cron timer.  The action is logged to the immutable audit
// trail regardless of whether the skill was previously enabled.
func disableReviewSkill(env *runtimeEnv, name string) error {
	entry, ok := env.Registry.Get(name)
	if !ok {
		return nil // skill not registered; nothing to do
	}

	// Update the disabled flag in metadata.
	meta := make(map[string]string, len(entry.Metadata)+1)
	for k, v := range entry.Metadata {
		meta[k] = v
	}
	meta[reviewMetadataKeyDisabled] = "true"

	if err := env.Registry.UpdateMetadata(name, meta); err != nil {
		return err
	}

	// Cancel the active cron timer for this skill.
	timers := env.EventBus.ListTimers(eventbus.TimerActive)
	for _, t := range timers {
		if t.Name == name && t.Owner == reviewTimerOwner {
			if _, err := env.EventBus.CancelTimer(t.TimerID); err != nil {
				env.Logger.Warn("review: failed to cancel timer for disabled skill",
					zap.String("skill", name),
					zap.String("timer_id", t.TimerID),
					zap.Error(err),
				)
			}
		}
	}

	// Audit-log the disable action.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"skill_name": name,
		"action":     "review_skill_disabled",
		"operator":   "operator",
	})
	act := kernel.NewAction(kernel.ActionSkillDeactivate, "operator", auditPayload)
	if _, err := env.Kernel.SignAndLog(act); err != nil {
		env.Logger.Error("review: failed to audit-log skill disable",
			zap.String("skill", name),
			zap.Error(err),
		)
	}

	env.Logger.Info("review: built-in review skill disabled",
		zap.String("skill", name),
	)
	return nil
}
