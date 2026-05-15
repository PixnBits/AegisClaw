# Directory & Filesystem Layout Specification

**Status:** Draft  
**Last Updated:** May 2026

## Goal
Provide a single, predictable, **security-first** home for AegisClaw while acknowledging that not everything can safely live under a user-controlled `~/.aegis/`. Paranoid security takes precedence over convenience.

## Core Principle
**User experience**: One primary directory (`~/.aegis/`) for most things.
**Security reality**: The two highest-risk items (privileged daemon socket + secrets vault) require extra protection beyond what a normal home directory provides.

## Recommended Layout (Security-Conscious)

```text
~/.aegis/                                   # Primary user root (most things live here)
├── config/                                 # Layered configuration
├── workspace/                              # AGENTS.md, SOUL.md, TOOLS.md, SKILL.md
├── cache/                                  # Downloads, builds, LLM caches
├── logs/                                   # Structured logs (0750)
├── git/                                    # Cloned skill/proposal repos (0750)
├── vm/                                     # Firecracker images & kernels (0750)
├── data/                                   # Persistent data
│   ├── store/                              # Encrypted Store VM backing (0700)
│   ├── audit/                              # Merkle tree roots + signed logs (0700)
│   ├── registry/                           # Skill registry & proposals
│   └── sbom/                               # Generated SBOMs
│
# === HIGH-SENSITIVITY ITEMS (extra protection required) ===
├── secrets/                                # Encrypted vault (see below)
│   └── vault.age                           # Main encrypted store
│
# Runtime / privileged items (NOT under ~/.aegis/)
/run/user/$UID/aegis/                       # Linux tmpfs (recommended for socket)
└── daemon.sock                             # Privileged daemon socket (0750, aesis group)
```

## Detailed Security Analysis & Recommendations

### 1. Daemon Socket (Highest Risk)

**Problem with `~/.aegis/socket/`**:
- `~/.aegis/` is fully user-controlled. A compromised user process (or malware) can:
  - `chmod 777 ~/.aegis`
  - Replace the socket with a symlink (TOCTOU attack)
  - Point it at sensitive files (`/etc/shadow`, etc.)
- Even with `SO_PEERCRED` + 0750, the **parent directory** remains an attack surface.

**Recommended Solution**:
- **Primary**: Use `/run/user/$UID/aegis/daemon.sock` on Linux (tmpfs, auto-cleaned on logout, root can still create it via capabilities or setuid helper).
- **Strong alternative**: Abstract Unix socket (`@aegis-daemon-$UID`) — no filesystem entry at all.
- **Fallback** (macOS/Windows): `~/.aegis/run/daemon.sock` with strict ACLs + runtime checks.

The socket **must never** live directly under `~/.aegis/` if the daemon runs with root privileges.

### 2. Secrets Vault (Critical)

**Risk if placed naively in `~/.aegis/secrets/`**:
- User (or attacker as user) can change permissions, replace the file, or read metadata.
- Even encrypted (`age`), offline attacks + metadata leakage are possible.

**Recommended Protections** (in order of strength):
1. **Runtime enforcement** (mandatory):
   - Daemon creates `~/.aegis/secrets/` as **0700** owned by daemon process / `aegis` group.
   - On **every access**: `open(..., O_NOFOLLOW)`, verify ownership + permissions, refuse + audit if wrong.
   - Use `fs.protected_regular` sysctl + `protected_fifos` on Linux.
2. **Preferred location** (stronger):
   - Move vault to `/var/lib/aegis/secrets/` (system directory, protected by root).
   - Or keep in `~/.aegis/secrets/` but treat it as "user-visible encrypted blob only" — actual decryption happens only inside the daemon after privilege checks.

### 3. Other Sensitive Directories

| Directory       | Recommended Location      | Permissions | Extra Protection                     |
|-----------------|---------------------------|-------------|--------------------------------------|
| `data/store/`   | `~/.aegis/data/store/`    | 0700        | Runtime ownership check on startup   |
| `data/audit/`   | `~/.aegis/data/audit/`    | 0700        | Merkle signing + runtime check       |
| `socket/`       | `/run/user/$UID/aegis/`   | 0750        | **Never under ~/.aegis/**            |
| `secrets/`      | `~/.aegis/secrets/` + runtime checks | 0700 | **Strongest possible enforcement**   |

## Implementation Rules

- The daemon **must** create all directories with correct permissions on first run.
- The daemon **must refuse to start** (or enter safe-mode) if `secrets/`, `data/store/`, or `data/audit/` have insecure permissions.
- All path constants live in `internal/paths/`.
- `aegis doctor --fix-permissions` must be able to repair common issues.

## Related Documents

- `host-daemon.md`
- `configuration-management.md`
- `secrets-vault.md` (future)
- `implementation-plan/05-unix-socket-hardening.md`
- `implementation-plan/06-directory-layout.md`

## Traceability
**Driven by:** Paranoid security analysis of home-directory attack surface + user request for single predictable root.