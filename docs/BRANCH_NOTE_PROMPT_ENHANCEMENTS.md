# Branch: feat/enhance-default-agent-prompts

This branch contains improvements to the default agent prompts (7 Court personas + Project Manager).

**Goal:** Make the default agents (Court personas and PM) aware of the full AegisClaw system functionality so they can be maximally useful to users.

Key areas incorporated into prompts:
- Paranoid isolation model (dedicated microVM sandboxes, AegisHub mediation with ACLs and signing)
- Turn-based channel collaboration (relevance_anchors, get_relevant_since, NO_REPLY discipline)
- Semantic tool discovery (tool.search)
- Governance Court + proposal workflow
- Memory VM, Store VM, Network Boundary
- SDLC orchestration and Builder VMs
- Web portal + #agents observability + STOMP
- Workspace AGENTS.md / SOUL.md customisation
- Security invariants (secrets never in prompts, Abstain on uncertainty)

Created 2026-06-27 via GitHub connector at user request.