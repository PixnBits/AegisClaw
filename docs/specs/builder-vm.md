# Builder VM Specification

**Status:** Draft  
**Last Updated:** June 2026

## Purpose

The Builder VM is a short-lived, untrusted microVM responsible for implementing an approved Change Proposal. It acts like a developer’s workstation — it can read, write, and push code, but cannot directly merge or bypass review.

The Store VM acts as the trusted git remote and PR manager.

## For Implementers

The Builder VM is the untrusted execution environment for code generation and skill implementation. When extending or implementing:
- Respect the strict "can / cannot" list below.
- All git and PR operations must go through the Store VM commands.
- Emit audit events and respect permission grants (see permissions-model.md).
- Ensure clean shutdown and no persistent state leakage.
- Test that a compromised or malicious Builder cannot merge, delete history, or fake Court reviews.

## Responsibilities

- Clone the skill repository from the Store VM
- Create a feature branch for the proposal
- Generate code using an LLM
- Commit and push changes to the Store VM
- Create a Pull Request when the implementation is ready for review
- Respond to Court feedback by pushing new commits
- Once approved, trigger the merge and build process
- Shut down cleanly when finished

## Git & PR Workflow

- The **Store VM** hosts all git repositories and manages Pull Request state
- The Builder VM can:
  - Clone repositories
  - Create branches
  - Commit and push code
  - **Create a Pull Request** (via explicit command)
- The Builder VM **cannot**:
  - Merge PRs
  - Delete branches
  - Modify existing PR reviews or approvals

## LLM Integration

The Builder VM uses a dedicated LLM instance for code generation. All prompts and responses are logged as part of the proposal for auditability.

## Communication

The Builder VM may only communicate with:
- **Store VM** — for cloning, pushing, creating PRs, and registering the final skill
- **Court Scribe VM** — for receiving reviews and feedback
- **AegisHub** — for routing

## Key Commands Used

- `store.git.clone`
- `store.git.push`
- `store.pr.create` ← Explicit command to open a Pull Request
- `store.pr.update`
- `store.skill.register`

## Security Requirements

- Builder VMs are explicitly untrusted
- Must run with minimal privileges
- Must not have direct filesystem access to host repositories
- All git operations go through the Store VM
- Must be terminated immediately after successful deployment or failure
- Permission grants and visibility policies (permissions-model.md) apply to Builder operations and any tool use inside the VM.

## Test Requirements

- Builder must not be able to merge its own PRs
- Builder must not be able to modify or fake Court reviews
- A crashed Builder VM must not leave the repository in a broken state
- All code changes must be traceable back to a specific proposal
- Permission and visibility filters must be enforced for any discovery or tool invocation inside the Builder.

