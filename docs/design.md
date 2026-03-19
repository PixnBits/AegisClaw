### Design Specifications – Human Interfaces

**Principles**  
- Transparency: Every Court decision, risk score, pros/cons visible.  
- Least Surprise: All actions require explicit confirmation for high-risk.  
- Approval Gates: Human is final sovereign (Enterprise Manual mode default).  
- Accessibility: CLI-first for Linux power users; optional local web UI.

**Primary Interfaces**  
1. **CLI / TUI** (`claw` binary – single static Go binary)  
   - `claw chat` → main agent REPL (ReAct loop).  
   - `claw propose skill "add Slack API"` → wizard: interactive refinement questions, risk sliders, About-Me alignment.  
   - `claw court status` / `claw court review <id>` → TUI table of reviewers (scores, evidence, questions). User votes approve/reject/ask-more.  
   - `claw status` / `claw rollback <mutation-id>` → dashboard of running skills, logs, audit grep.  
   - `claw sandbox list` / `claw secret add` (vault integration).  

2. **Local Web Dashboard** (optional, served from kernel on 127.0.0.1:port with self-signed TLS)  
   - Proposal board (NASA-style table).  
   - Live skill map + isolation status.  
   - Audit timeline viewer.  
   - Built with lightweight Svelte + local auth (TOTP optional).

3. **Proposal Flow UX**  
   - Step-by-step wizard with progress bar.  
   - Real-time Court progress (spinner + partial results).  
   - Diff viewer for code changes before approval.  
   - Risk heatmap (red/yellow/green) from CISO persona.

**Modes** (configurable via Court)  
- Hobbyist Auto (LLM-only reviews).  
- Enterprise Manual (human final vote on every mutation).
