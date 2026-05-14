# AegisClaw Documentation

Welcome to the **strengthened, modular documentation** for AegisClaw v2.

This structure was developed based on lessons learned during implementation. It is designed to be easier to navigate, maintain, and keep in sync with the codebase.

## Quick Navigation

| Document                  | Purpose                                      | Location |
|---------------------------|----------------------------------------------|----------|
| **Roadmap**               | Phased development plan + "test every user journey" rule | [roadmap.md](roadmap.md) |
| **Testing Standards**     | Required test coverage, CI rules, and philosophy | [testing-standards.md](testing-standards.md) |
| **First Skill Tutorial**  | Step-by-step guide to creating your first skill (highly recommended) | [guides/first-skill-tutorial.md](guides/first-skill-tutorial.md) |
| **Product Requirements**  | Modular PRD (vision, personas, security, governance, etc.) | [prd/](prd/) |
| **Technical Specs**       | Detailed component specifications | [specs/](specs/) |
| **Implementation Plan**   | Numbered step-by-step tasks by phase | [implementation-plan/](implementation-plan/) |
| **Tasks**                 | Phase-specific task breakdowns | [tasks/](tasks/) |
| **Changelog**             | Recent phases and major feature history | [CHANGELOG.md](CHANGELOG.md) |
| **Architecture**          | High-level system architecture | [architecture.md](architecture.md) |

## Structure Overview

- **`prd/`** — Product Requirements Document broken into focused, maintainable files
- **`specs/`** — Technical specifications for core components (agent runtime, governance court, builder VM, memory, network boundary, etc.)
- **`implementation-plan/`** — 25 numbered implementation steps with clear dependencies
- **`guides/`** — User-facing tutorials and how-to guides
- **`tasks/`** — Actionable task lists tied to roadmap phases

## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on keeping the documentation consistent and up to date.

---

*This documentation structure replaces the previous flat collection of files for better long-term maintainability.*

