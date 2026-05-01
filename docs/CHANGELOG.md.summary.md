# `docs/CHANGELOG.md` — Summary

## Purpose

Tracks completed and in-progress feature deliveries across AegisClaw's phased development roadmap. Organised by phase (unreleased phases listed newest-first), with each entry describing new packages, configuration additions, CLI changes, and behavioural changes introduced in that phase.

## Coverage

| Phase | Key Additions |
|---|---|
| Phase 6 (unreleased) | `internal/sbom` (CycloneDX 1.4 SBOMs), `internal/memory/pii.go` (PII redaction with 7 regex rules), dashboard overview and skills pages |
| Phase 5 | `internal/eval` — synthetic evaluation harness with three agentic scenarios |
| Phase 4 | `internal/dashboard` — local web portal (HTMX-free Go templates, SSE live updates, in-browser approvals) |
| Phases 3, 2, 1, 0 | (Implied by ordered phase structure; earlier phases cover kernel, court, builder, sandbox, audit, composition, worker, gateway, etc.) |

## Notable Entries (Phase 6)

- **SBOM**: `Generate(BuildInfo)` + `Write(dir, *SBOM)` called at builder step 9.5; `skill.sbom` tool; `aegisclaw skill sbom` CLI command; config key `builder.sbom_dir`.
- **PII Redaction**: `Scrubber` with email, phone, SSN, IPv4, JWT, AWS key, and generic API key patterns; hooked into `Store.Store()` when `memory.pii_redaction: true`.
- **Dashboard**: New overview page with quick-stats cards; skills/proposals listing page; updated navigation; privacy controls in settings.

## Fit in the Broader System

Provides a developer-facing record of what has been shipped and where new code lives. Paired with `docs/prd-deviations.md` (alignment status) and `docs/implementation-plan.md` (phased roadmap). Each entry cross-references the relevant `internal/` package.
