### Product Requirement Document (PRD) – AegisClaw v1.0

**Vision**  
A paranoid-by-design, local-first, self-evolving AI agent platform that runs exclusively on Linux with Ollama. Skills are added/extended only through an enterprise-grade SDLC (refinement → plan → code → multi-persona review incl. CISO → git PR → build/test → final approval → isolated deployment). API keys never appear in LLM prompts or between skills. Antagonistic skills are impossible due to per-skill microVM isolation.

**Target Users**  
- Solo power users / researchers (hobbyist → enterprise modes).  
- Persona: Alex Rivera – security-conscious Linux admin who wants Slack/GitHub/etc. tools without trusting any cloud or unvetted code.

**Key Features & User Flows** (exact Slack tool example)  
1. User: “Add a tool to send messages to Slack.”  
2. Agent (in main sandbox) refines via interactive questions (business need, auth method, rate limits, risks).  
3. Proposal JSON generated + stored in git.  
4. Governance Court spins up isolated reviewers (LLM personas: Coder, Tester, CISO, Security Architect, User Advocate) – each in own microVM.  
5. CISO persona enforces: network policy, secret proxy config, threat model.  
6. Code generated/edited in builder sandbox → git commit + simulated PR.  
7. Reviewers iterate (approve/reject/ask questions) until consensus or user override.  
8. Build/test in disposable sandbox → artifact signed.  
9. Deployment proposal (Docker Sandbox compose update) → final user vote.  
10. Activate: skill starts in isolated microVM; tool immediately usable.  
All reversible (snapshots + git revert).

**Non-Functional Requirements (Security-First)**  
- **Isolation**: Docker Sandboxes microVMs (primary) or Firecracker (opt-in). Read-only FS, cap-drop ALL, private Docker daemon, no shared memory.  
- **Secrets**: Docker Sandboxes network proxy (or SOPS/age + ephemeral mount) – keys injected at runtime, never in prompts, code, logs, or LLM context.  
- **LLM Trust**: Ollama-only. Default ensemble: Llama-3.2-3B (fast reviewers), Mistral-Nemo (reasoning), Phi-3 (small audited). Downloads verified + hash-checked. Cross-verify Court outputs with ≥2 models. No cloud APIs.  
- **Audit**: Append-only Merkle-tree log (every proposal, review, git change, deployment). Tamper-evident.  
- **Reliability/Predictability**: 98% structured JSON parsing; all outputs schema-validated; reversible mutations.  
- **Extensibility**: Pluggable sandbox backends; new reviewer personas via Court config.  
- **Resource**: <4 GB RAM baseline; low-resource sequential mode.  

**Success Metrics**  
- Zero isolation violations in red-team tests.  
- Every skill addition completes end-to-end in <15 min (user time).  
- Self-improvement: system can propose and apply its own patches via Court.
