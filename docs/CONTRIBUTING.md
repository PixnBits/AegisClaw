# Contributing to AegisClaw Documentation

Thank you for helping keep the AegisClaw docs accurate, useful, and easy to maintain!

## Core Principles

- **Modular first** — Keep the `prd/`, `specs/`, and `implementation-plan/` folders modular. One focused topic per file is preferred.
- **User journeys are sacred** — Every new feature or phase should have corresponding automated tests. Update `roadmap.md` and `testing-standards.md` when this changes.
- **Docs should be living** — Update documentation at the same time as code changes whenever possible.

## Where to Put Things

| Type of Content                  | Location                          | Notes |
|----------------------------------|-----------------------------------|-------|
| Product vision, goals, personas  | `prd/`                            | Use `index.md` as the entry point |
| Technical component specs        | `specs/`                          | One file per major component |
| Step-by-step implementation      | `implementation-plan/`            | Numbered files (e.g. `01-...md`) |
| User tutorials & how-tos         | `guides/`                         | Practical, example-driven guides |
| Phase-level tasks                | `tasks/`                          | Keep high-level and actionable |
| Recent history & releases        | `CHANGELOG.md`                    | Follow the existing phase format |
| Architecture overview            | `architecture.md`                 | Keep high-level |

## Guidelines

- **Links** — Use relative links (`[roadmap](roadmap.md)`) so they work both on GitHub and when rendered.
- **Examples** — All CLI and chat examples should be tested before committing.
- **New phases** — When a new phase is added to the roadmap, create the corresponding section in `implementation-plan/` and update `roadmap.md`.
- **Security & privacy** — Any changes to memory, secrets, governance, or network policy must be reflected in the relevant `prd/` and `specs/` files.
- **Tutorials** — The `guides/first-skill-tutorial.md` is the primary onboarding document. Keep it up to date with the current skill creation flow.

## Before Opening a PR

- [ ] All user journeys mentioned are covered by tests (or noted as future work)
- [ ] `CHANGELOG.md` is updated for any new phases or major features
- [ ] Links in `README.md` still resolve correctly
- [ ] No broken relative links

## Questions?

Open an issue, start a discussion, or ask in the AegisClaw chat. We’re happy to help!

---

*Thank you for contributing to better documentation!*

