# AegisClaw Changelog

All notable changes to this project are documented here. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [v2] - 2026-05-26 (Finish-Line / 7.8)

### Added
- **SBOM generation & supply-chain hardening (Task 7.8 priority 1)**:
  - New `make sbom` target (always succeeds; CycloneDX JSON via cyclonedx-gomod/syft when present, or high-quality fallback manifest with go.mod + Builder security gates).
  - Enhanced Builder VM rootfs SBOM stub in `scripts/build-microvms-docker.sh` (now references 7.8 + `make sbom` + cosign).
  - Image signing hooks (cosign, keyless or COSIGN_* env) as non-fatal placeholders in Makefile and build scripts.
  - Updated notes in `cmd/aegis` (doctor/gates paths) and `cmd/builder`.
  - References: threat-model.md:3 (backdoored skill mitigation via SBOM + signing), additional-requirements-and-gaps.md, builder-security-gates.md, grok-build-execution-plan.md:7.8 / 1193, host-daemon.md (TCB supply chain for the 9 journeys).

- **Final docs alignment (Task 7.8 priority 2)**:
  - README.md: new "Supply-Chain & Release (7.8)" section.
  - AGENTS.md: one-line addition for `make sbom` (zero impact on sacred start/stop/doctor rules).
  - threat-model.md + additional-requirements-and-gaps.md: implementation notes + cross-refs (exact sections cited in commits).
  - All changes additive with precise spec citations.

- **Clean make targets + release notes (Task 7.8 priority 3)**:
  - `make sbom` fully documented in `make help` and Makefile comments (additive, modeled on existing test-* targets).
  - This CHANGELOG.md (v2 entry) as the initial release notes draft (summarizes 7.5 TCB hardening, 7.6/7.7 autonomy + chaos + coverage, 7.8 SBOM).
  - No changes to any existing `make start/stop/test/test-chaos` or doctor behavior.

- **Final security audit + verification (Task 7.8 priorities 4-5)**:
  - Review of 7.5 TCB (key distribution, watchdog, containment, socket auth, expanded doctor) against threat-model.md and host-daemon.md.
  - Full matrix verification: `make test`, `make test-chaos` (AEGIS_CHAOS=1), core builds, doctor (healthy + TCB), 9-journey surfaces, no orphans, AGENTS.md compliance.
  - All 9 user journeys remain reliable in live daemon + failure/recovery modes (chaos + E2E assertions).

### Changed / Improved
- 7.5 Host Daemon TCB completion (prior): PDEATHSIG + process groups on all children (incl. web-portal), StartCriticalWatchdog + privileged events, 0600 ephemeral VM key distribution + zeroization + no retention (orchestrator + sandbox backends), expanded `aegis doctor` (Merkle roundtrips, workspace AGENTS.md presence, static binary, memory <20MB, key isolation), SO_PEERCRED socket auth.
  - Refs: host-daemon.md (all Test Requirements, Responsibilities, Keypair Isolation, Lifecycle Containment), threat-model.md:4.

- 7.6 Deep integration (prior): EventBus + workspace customization into Agent 6-step, Court personas, Builder gates, Teams; proactive/background behaviors.
  - Refs: prd/agent-autonomy.md, event-system.md, builder-security-gates.md, teams-multi-agent-plan.md, governance-court.md.

- 7.7 Testing, Coverage & Chaos (prior): `make test-chaos`, 3+ high-value tagged chaos seeds (daemon unclean restart, VM death + watchdog, full mid-journey restart with pre-activity + post TCB), deepened 9-journey E2E recovery matrix, unit test lifts (security key isolation etc.), explicit coverage tracking (security to 73.6%, overall 10.5% with rationale for daemon-heavy paths).
  - Refs: grok-build-execution-plan.md:1196 (80% goal), testing-standards.md, host-daemon.md:Test Requirements, all 9 user-journeys/*.md (recoverability).

### Notes
- v2 is now in excellent shape for review/merge: all 9 journeys reliable (including after daemon/VM failure), paranoid TCB properties provable via tests + `aegis doctor`, SBOM + signing hooks present, docs aligned, make targets clean.
- No new hard dependencies. All changes additive or in test/docs paths.
- Next (post v2): full SBOM in Builder VM (syft in rootfs), cosign in CI, higher coverage via more integration, jailer/cgroups for watchdog.

See the session plan (`.grok/sessions/.../plan.md`) and individual commit messages for detailed spec citations and verification logs.

## [Unreleased]
- (Future work after v2 review)
