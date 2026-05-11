# Web Portal Screens & Design Specification

## Design Philosophy
- Dark, clean, high-contrast "secure command center" aesthetic
- Paranoid by design: transparency, clear status, prominent emergency controls
- Fully self-contained (no external CDNs, fonts, or assets)
- Prioritizes clarity, fast feedback (RAIL), and easy access to critical actions

## Color Palette
- **Background**: `#0a0a0a`
- **Surface / Cards**: `#1f1f1f`
- **Elevated**: `#2a2a2a`
- **Accent / Primary**: `#00d4ff`
- **Success**: `#22ff88`
- **Warning**: `#ffcc00`
- **Danger / Safe Mode**: `#ff3333`
- **Text Primary**: `#e0e0e0`
- **Text Secondary**: `#aaaaaa`
- **Border**: `#333333`

## Typography
- Primary: `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`
- Monospace: `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace`

## Consistent Header (All Screens)
```
[Logo: AegisClaw]   [System Status: ● Daemon Running | Firecracker]   [Navigation]   [Connection Status] [Notifications (n)] [Avatar ▼]
```

**Navigation**: Dashboard • Conversations • Teams • Agents • Skills • Court • Monitoring • Audit

**Right side** (right to left):
- Avatar → dropdown (About Me, Settings, Agent Customization)
- Notifications (bell with badge)
- Connection status (WebSocket + SSE)

---

## Core Screens

### 1. Dashboard (Home)
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     Dashboard                             Conn ●  (2)  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Conversations  |  Dashboard                                       | Context Panel      │
│────────────────┼──────────────────────────────────────────────────┼────────────────────┤
│                │  Quick Actions                                   │ Quick Stats        │
│   All Chats    │  [New Chat]   [New Team]   [Propose Skill]       │ Active Agents: 3   │
│   • researcher │                                                  │ Background Tasks: 2│
│   • analyst    │  Active Agents                                   │ Skills Installed: 24│
│   • general    │  researcher   ● Working on Zig analysis (4m)    │                    │
│                │  analyst      ○ Idle                             │                    │
│                │  general      ● Monitoring emails                │                    │
│                │                                                  │                    │
│                │  Background Tasks                                │ Recent Activity    │
│                │  • Research Zig adoption (12% complete)          │ • discord_monitor  │
│                │  • Daily security scan (running)                 │   deployed v1.2    │
│                │                                                  │ • Court approved   │
│                │  System Health (lower)                           │   web_search v2    │
│                │  • Daemon      ● Running                         │                    │
│                │  • Firecracker ● Ready                           │                    │
├────────────────┴──────────────────────────────────────────────────┴────────────────────┤
│  Safe Mode is OFF   [Enable Safe Mode]                                               │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### 2. Chat Interface
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     researcher @ general                  Conn ●  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Conversations  |  researcher                                      | Context Panel      │
│────────────────┼──────────────────────────────────────────────────┼────────────────────┤
│                │  [Agent Message]                                 │ Current Agent      │
│   All Chats    │  Thinking... (Observe → Think → Plan)  1.8s     │ • researcher       │
│   • researcher │                                                  │ Autonomy: Research │
│   • analyst    │  → tool.search "latest AI security news"         │ Mode               │
│                │  → web_search.execute (2.4s)                     │                    │
│                │                                                  │ Recent Tools       │
│                │  [Streaming Response - incremental Markdown]     │ • web_search       │
│                │  Rust 1.86 introduces ...                        │ • code_execution   │
│                │                                                  │                    │
│                │  [User Message - right aligned]                  │                    │
│                │  What are the security implications?             │                    │
│                │                                                  │                    │
│                │  [Input Box] Send                                │                    │
├────────────────┴──────────────────────────────────────────────────┴────────────────────┤
```

### 3. Team Workspace
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     Zig Adoption Analysis                 Conn ●  (2)  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Conversations  |  Teams                                           | Context Panel      │
│────────────────┼──────────────────────────────────────────────────┼────────────────────┤
│                │  Goal: "Analyze pros/cons of adopting Zig..."    │ Active Agents (4)  │
│   All Teams    │  Status: In Progress                             │ • researcher       │
│   • Zig Adoption (Active)                                │ • analyst          │
│   • Q3 Planning                                         │ • coder            │
│   • Security Audit                                      │ • critic           │
│                │                                                  │                    │
│                │  researcher: "I've gathered 12 recent papers..." │ Shared Context     │
│                │  analyst: "Key tradeoffs: memory safety vs perf" │ • Requirements     │
│                │  coder: "Here's a minimal viable example..."     │ • Constraints      │
│                │  critic: "Security concerns with package mgmt"   │ • Timeline         │
│                │                                                  │                    │
│                │  [Live Timeline / Activity Feed]                 │                    │
│                │  • researcher updated findings (2m ago)         │                    │
│                │  • coder submitted code snippet                  │                    │
│                │                                                  │                    │
├────────────────┴──────────────────────────────────────────────────┴────────────────────┤
│  @researcher What do you think about the memory safety claims?     [Send]           │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### 4. Court / Governance
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     Court / Governance                    Conn ●  (1)  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Conversations  |  Court                                           | Context Panel      │
│────────────────┼──────────────────────────────────────────────────┼────────────────────┤
│                │  Recent Decisions                                       │ Pending Proposals  │
│   All Decisions│                                                          │ (2)                │
│   • Approved   │  Proposal: discord_monitor v1.2                          │                    │
│   • Rejected   │  Status: APPROVED • 7/7 Unanimous                       │ 1. web_search v2   │
│   • Pending    │  Deployed: 2026-05-09 14:32                             │ 2. email_client    │
│                │                                                          │                    │
│                │  [View Code Changes / Diff]   [Build Logs]   [SBOM]    │                    │
│                │  Security Gates: All Passed                              │                    │
│                │  • SAST Passed   • SCA Passed   • Secrets Scan Passed   │                    │
│                │                                                          │                    │
│                │  Proposal: web_search v2                                 │ Court Status       │
│                │  Status: UNDER REVIEW                                    │ • CISO      Voted  │
│                │  Votes: 4/7 Approved • 2 Reject • 1 Abstain             │ • Security Architect Voted │
│                │                                                          │ • Architect Voted  │
│                │  [View Full Proposal + Code Diff]   [Build Logs]        │                    │
│                │                                                          │                    │
│                │  [Propose New Skill]                                     │                    │
├────────────────┴──────────────────────────────────────────────────┴────────────────────┤
│  Search proposals...                                                                │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### 5. Skills Registry
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     Skills Registry                       Conn ●  (4)  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Conversations  |  Skills                                          | Context Panel      │
│────────────────┼──────────────────────────────────────────────────┼────────────────────┤
│                │  Installed Skills (24)                           │ Quick Actions      │
│   All Skills   │                                                  │ [Propose New Skill]│
│   • Enabled    │  discord_monitor          v1.2     ● Deployed   │                    │
│   • Disabled   │  web_search               v2.1     ● Deployed   │                    │
│   • Proposed   │  github_pr_reviewer       v1.0     ● Deployed   │                    │
│                │  email_client             v0.9     ○ Building   │                    │
│                │                                                  │                    │
│                │  ─────────────────────────────────────────────── │                    │
│                │  Skill: discord_monitor                          │ Skill Details      │
│                │  Description: Monitor Discord servers for        │ • Version: 1.2     │
│                │               keywords and send summaries        │ • Status: Deployed │
│                │  Required Scopes: network:discord.com, background│ • Last Updated     │
│                │  Secrets: DISCORD_BOT_TOKEN                      │   2026-05-09       │
│                │                                                  │                    │
│                │  [View Code]  [Build Logs]  [Security Gates]     │                    │
│                │  [Disable]    [Update]                           │                    │
│                │                                                  │                    │
│                │  Search skills...                                │                    │
├────────────────┴──────────────────────────────────────────────────┴────────────────────┤
│  Filter: All • Enabled • Disabled • Proposed                                         │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### 6. Monitoring / Status
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     Monitoring                            Conn ●  (5)  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Conversations  |  Monitoring                                      | Context Panel      │
│────────────────┼──────────────────────────────────────────────────┼────────────────────┤
│                │  Active Agents (3)                               │ Global Stats       │
│   All Tasks    │  researcher   ● Working on Zig analysis (87%)   │ Running VMs: 4     │
│   • Running    │  analyst      ○ Idle (last active 12m ago)       │ Background Tasks: 7│
│   • Paused     │  general      ● Monitoring emails (42% done)    │ CPU Usage: 34%     │
│                │                                                  │ Memory Usage: 18GB │
│                │  Background Tasks                                │                    │
│                │  • Research Zig adoption          87% (4m ago)  │                    │
│                │  • Daily security scan            23%           │                    │
│                │  • Email digest generation        Paused        │                    │
│                │                                                  │                    │
│                │  [Pause All]  [Resume All]  [Cancel All]         │                    │
│                │                                                  │                    │
│                │  Live Logs (last 50 lines)                       │                    │
│                │  14:32:11 researcher: Found 12 relevant papers   │                    │
│                │  14:32:09 analyst: Key tradeoff identified       │                    │
│                │  14:31:55 general: New email batch processed     │                    │
├────────────────┴──────────────────────────────────────────────────┴────────────────────┤
│  Safe Mode is OFF   [Enable Safe Mode]                                               │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

### Proposal/PR Screen
```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ AegisClaw   ● Running (Firecracker)     Proposal: discord_monitor v1.2        Conn ●  [Avatar ▼] │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ Court  |  Proposal Detail                                      | Review Panel       │
│────────┼───────────────────────────────────────────────────────┼────────────────────┤
│        │  Status: UNDER REVIEW • Proposed by researcher        │ Current Votes      │
│        │  Submitted: 2026-05-09 11:42                          │ • CISO          Approve    │
│        │  Target Skill: discord_monitor                        │ • Security Arch Approve    │
│        │                                                       │ • Architect     Approve    │
│        │  [Code Changes]   [Build Logs]   [Security Gates]     │ • Senior Coder  Reject     │
│        │  [SBOM]   [Download Artifact]                         │ • Tester        Approve    │
│        │                                                       │ • Efficiency    Abstain    │
│        │  Overall: 4 Approved • 1 Reject • 1 Abstain           │ • User Advocate Approve    │
│        │                                                       │                    │
│        │  ──────────────────────────────────────────────────── │                    │
│        │  Files Changed (3)                                    │                    │
│        │  • SKILL.md          +124 −12                         │                    │
│        │  • main.go           +87 −3                           │                    │
│        │  • discord_client.go +56 −0                           │                    │
│        │                                                       │                    │
│        │  [Unified Diff View]                                  │                    │
│        │  + func StartMonitoring(...) {                        │                    │
│        │  ...                                                  │                    │
│        │                                                       │                    │
│        │  Comments & Reviews                                   │                    │
│        │  • Security Architect (2h ago)                        │                    │
│        │    "LGTM after rate limiting fix"                     │                    │
│        │  • CISO (1h ago)                                      │                    │
│        │    "Approved - business risk acceptable"              │                    │
│        │                                                       │                    │
│        │  [Add Comment]                                        │                    │
├────────┴───────────────────────────────────────────────────────┴────────────────────┤
│  Overall Verdict: Needs 2 more approvals for merge                                   │
└──────────────────────────────────────────────────────────────────────────────────────┘
```
