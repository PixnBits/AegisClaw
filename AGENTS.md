# AGENTS.md — Guidance for AI Agents & Developers

This file helps AI coding agents (and human developers) work effectively with the AegisClaw codebase and its documentation.

## Core Philosophy

AegisClaw is a **paranoid-by-design**, security-first agent platform that runs skills in isolated Firecracker microVMs with Governance Court review for every change.

**Golden Rule**: No feature is considered done until its corresponding user journey has automated tests.

## When Given a Task, Follow This Order

1. **Read the Roadmap**  
   `docs/roadmap.md` — Understand the current phase and which user journeys matter most right now.

2. **Check Testing Standards**  
   `docs/testing-standards.md` — Know the minimum coverage and test requirements before starting.

3. **Understand the Relevant Area**
   - New feature or capability → Start with the appropriate file in `docs/specs/`
   - Product direction or requirements → `docs/prd/`
   - Implementation steps → `docs/implementation-plan/`
   - User onboarding or tutorials → `docs/guides/first-skill-tutorial.md`

4. **Review Related Docs**
   - Architecture overview: `docs/architecture.md`
   - Memory system details: `docs/specs/memory-store.md` and `docs/specs/memory-vm.md`
   - Governance & security: `docs/prd/security-model.md`, `docs/prd/governance-court.md`, `docs/specs/governance-court.md`

5. **Propose Changes the AegisClaw Way**
   - Use the proper skill/proposal flow when modifying behavior
   - All significant changes should go through the Governance Court (even for internal tools)
   - Update `CHANGELOG.md` for any new phases or major features

## Key Files Every Agent Should Know

| File                              | Purpose                                      | Read When |
|-----------------------------------|----------------------------------------------|---------|
| `docs/roadmap.md`                 | Phased plan + user journey testing rule      | Always at the start of a task |
| `docs/testing-standards.md`       | Test coverage, CI, and quality rules         | Before writing or modifying code |
| `docs/prd/index.md`               | Product vision and high-level requirements   | New features or major changes |
| `docs/specs/`                     | Detailed technical specifications            | Implementing or modifying components |
| `docs/implementation-plan/`       | Numbered step-by-step tasks                  | Planning implementation work |
| `docs/guides/first-skill-tutorial.md` | Practical skill creation walkthrough     | Working on skill-related features |
| `CHANGELOG.md`                    | Recent phase history                         | Understanding current state of the project |

## Important Rules for Agents

- **Never bypass security gates** in the builder pipeline, even during development.
- **Always think in terms of user journeys** (see `roadmap.md` for the current list).
- **Prefer modular updates** — update only the relevant file in `prd/`, `specs/`, or `implementation-plan/` rather than writing large monolithic changes.
- **Document as you go** — if you add or change behavior, update the corresponding spec or PRD section.
- **Use the existing patterns** — look at how similar features were implemented in previous phases.

## Quick Commands for Context

```bash
# See current status and active proposals
./aegisclaw status

# View recent audit log
./aegisclaw audit log --limit 20

# Run evaluation harness (very useful for testing)
./aegisclaw eval run
```

---

**Goal**: Make every agent (human or AI) faster and safer by pointing them to the right documentation at the right time.

If you're an AI agent reading this, start with the **Roadmap** and **Testing Standards** before writing any code.

