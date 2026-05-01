# first_skill_tutorial_test.go — cmd/aegisclaw

## Purpose
End-to-end journey test mirroring the `docs/first-skill-tutorial.md` walkthrough: create a "time-of-day greeter" skill via the chat handler, verify the spec, submit for Governance Court review, and confirm approval. Does not require KVM.

## Key Helpers / Tests
- `TestFirstSkillTutorialJourney` — drives the full create → spec → submit → court-review flow using a scripted executor and real kernel + proposal + court stores in temp dirs.
- Uses `securitygate.ReviewSkillProposal` to confirm the court engine correctly approves the benign skill.

## System Fit
Acceptance test for the complete SDLC path. Passes in standard `go test` since it uses in-memory/temp-dir implementations.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/builder`
- `github.com/PixnBits/AegisClaw/internal/court`
- `github.com/PixnBits/AegisClaw/internal/kernel`
- `github.com/PixnBits/AegisClaw/internal/proposal`
