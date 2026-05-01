# init_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw init` command. Creates the directory structure, writes the default configuration, and initialises the kernel (Ed25519 signing key pair + bootstrap audit log entry).

## Key Types / Functions
- `runInit(cmd, args)` — resolves workspace directory, creates required subdirs, calls `kernel.Init`, writes config, prints next steps.
- Profile selection: `--profile hobbyist|startup|enterprise` maps to strictness levels `low|medium|high`.
- Idempotent: re-running on an existing workspace updates the profile without re-generating the key pair.

## System Fit
Must be run before any other command. The kernel key pair created here is referenced by `audit_verify.go` and `start.go`.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel` — `kernel.Init`
- `github.com/PixnBits/AegisClaw/config` — config writer
