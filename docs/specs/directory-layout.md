# Directory & Filesystem Layout Specification

**Status:** Draft  
**Last Updated:** May 2026

## Goal
Provide a single, predictable, security-first home for all AegisClaw data under `~/.aegis/`. This avoids user surprise from scattered directories while enforcing **paranoid security** (least privilege, no unnecessary exposure, clear separation of concerns).

## Root Directory

**Location:** `~/.aegis/` (per-user, XDG-compliant where possible)

All components **must** live under this single root unless a strong security or platform reason exists (e.g., `/run/user/$UID/aegis/` for runtime state on Linux).

## Recommended Layout

```text
~/.aegis/
├── config/                  # Layered configuration (see configuration-management.md)
│   ├── config.yaml          # Main user config
│   └── profiles/            # Per-agent or per-environment overrides
├── socket/                  # Hardened Unix socket(s) — see step 05
├── vm/                      # Firecracker / Docker Sandbox images & kernels
│   ├── images/              # .img, .ext4, rootfs snapshots
│   └── kernels/             # vmlinux, initrd
├── git/                     # Cloned skill repositories & proposals (read-only where possible)
├── logs/                    # Structured JSON logs + rotation (0700 recommended)
├── data/                    # Persistent application data
│   ├── store/               # Encrypted Store VM backing
│   ├── audit/               # Merkle tree roots + signed audit logs
│   ├── registry/            # Skill registry, proposals, composition history
│   └── sbom/                # Generated SBOMs (CycloneDX)
├── secrets/                 # Encrypted secrets vault (age + HKDF)
│   └── vault.age            # Main encrypted store (0700, daemon/group owned)
├── workspace/               # User customization files (AGENTS.md, SOUL.md, TOOLS.md, SKILL.md)
├── cache/                   # Temporary downloads, compiled artifacts, LLM caches
└── run/                     # Runtime state (locks, PID files, temp sockets)
    └── (symlink to /run/user/$UID/aegis/ on Linux for better security)
```

## Security Requirements (Paranoid-by-Design)

| Directory     | Permissions | Ownership                  | Rationale                                      |
|---------------|-------------|----------------------------|------------------------------------------------|
| `secrets/`    | 0700        | Daemon process / `aegis` group | Never world-readable; only daemon may write   |
| `data/store/` | 0700        | Daemon / `aegis` group     | Encrypted persistent state; tamper-evident    |
| `data/audit/` | 0700        | Daemon / `aegis` group     | Merkle tree must be protected                 |
| `socket/`     | 0750        | `aegis` group              | Follows Unix Socket Hardening (step 05)       |
| `logs/`       | 0750        | `aegis` group              | Readable by operators, not world-readable     |
| `workspace/`  | 0755        | User                       | User-editable customization files             |
| `vm/`         | 0750        | `aegis` group              | VM images should not be world-readable        |
| `git/`        | 0750        | `aegis` group              | Cloned repos may contain untrusted code       |

- All sensitive directories **must** be created with correct permissions on first run.
- The daemon **must** refuse to start if permissions are too permissive on `secrets/` or `data/store/`.
- Use `umask 0077` or explicit `os.Mkdir` with mode when creating directories.
- Never store secrets or private keys outside `secrets/`.

## Platform Notes

- **Linux**: Prefer `/run/user/$UID/aegis/` for `run/` (tmpfs, auto-cleaned on logout).
- **macOS/Windows**: Fall back to `~/.aegis/run/` with appropriate ACLs.
- XDG Base Directory spec is followed for `config/`, `cache/`, `data/` where it does not conflict with security grouping.

## Related Documents

- `host-daemon.md` (Unix socket & lifecycle)
- `configuration-management.md` (already references `~/.aegis/config.yaml`)
- `secrets-vault.md` (future)
- `implementation-plan/05-unix-socket-hardening.md`
- `implementation-plan/06-directory-layout.md` (paired task)

## Traceability
**Driven by:** User request for single predictable root + paranoid security requirement to minimize attack surface and user surprise.