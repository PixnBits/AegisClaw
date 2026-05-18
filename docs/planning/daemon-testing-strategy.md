## Recommended CI / PR Checks

**Always run (fast feedback):**
- Build + Vet
- `make test` (unit + basic hardening tests)

**Opt-in / Manual trigger:**
- `make test-integration` (richer lifecycle containment tests)
- Fuzz testing (when implemented)

This keeps PRs fast while still allowing thorough local + advanced verification.