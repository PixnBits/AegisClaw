# `docs/PRD.md` — Summary

## Purpose

The authoritative Product Requirements Document for AegisClaw v2.0 — a paranoid-by-design, local-first, self-evolving AI agent platform for Linux. Defines the product vision, problem statement, target users, security principles, and feature requirements across all phases.

## Key Sections

- **Executive Summary**: AegisClaw as a secure-by-design local agent; skills run in per-skill microVMs; API keys never appear in prompts, code, logs, or LLM context.
- **Problem Statement**: Credential leaks, lack of governance in existing agents, non-reversible actions, cloud dependency.
- **Key Differentiators**: Governance Court (5 AI personas), Firecracker isolation, Merkle-tree audit log, git-backed deployments, local-first Ollama.
- **Target Users**: Hobbyists → Startups → Enterprises (all on the same Linux + Ollama foundation).
- **Security Principles**: Every skill is a potential threat; default-deny networks; `cap-drop ALL`; secrets via proxy only; mandatory SAST/SCA/secrets scanning.
- **Governance Court**: 5 AI personas (CISO, SeniorCoder, SecurityArchitect, Tester, UserAdvocate) each running in isolated microVMs; weighted consensus; no bypass mechanism.
- **CLI Requirements**: `init`, `start`, `stop`, `status`, `chat`, `skill`, `audit`, `secrets`, `self`, `version`.
- **Phases**: 0 (foundations) through 6 (security hardening, SBOM, PII).

## Fit in the Broader System

The PRD is the north-star requirements document. All implementation decisions trace back to it. Deviations are tracked in `docs/prd-deviations.md`. The architecture that realises these requirements is described in `docs/architecture.md`. The phased delivery plan is in `docs/implementation-plan.md`.
