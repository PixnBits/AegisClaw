# 01 - First-time Installation & Onboarding

## Overview
The first-time developer experience must be simple, transparent, and fully scriptable. The entire flow should be automatable for integration tests and CI.

## Prerequisites
- **Docker** installed and running
- **Ollama** installed and running with at least one model available (`ollama list` succeeds)
- **Git** installed
- **Go 1.23+** installed (for building from source)
- User has full administrative/sudo access on the machine
- ~2 GB free RAM and 10 GB free disk space

## User Story
As a developer, I want to clone the repository, run setup, verify the system is healthy, and start my first conversation.

## Success Criteria (Testable)
- `aegis doctor` returns exit code 0 and reports "All systems healthy"
- `aegis status` shows Host Daemon running, at least one Court persona online, and sandbox backends ready
- `aegis chat --headless "Say hello"` completes successfully and returns a response
- All core components (Host Daemon, AegisHub, Court Scribe) start without errors
- No manual intervention required after `make setup` or equivalent

## Step-by-Step Flow

1. **Clone & Enter Directory**
   - `git clone https://github.com/PixnBits/AegisClaw.git && cd AegisClaw`

2. **Verify Prerequisites**
   - `./scripts/doctor.sh` or `go run ./cmd/aegis doctor`
   - Script checks Docker, Ollama, Go version, permissions, and available ports

3. **Build & Install**
   - `make build` or `go build -o bin/aegis ./cmd/aegis`
   - `make install` (installs host daemon as a service/systemd unit or background process)

4. **Start Core System**
   - `aegis start` or `make start`
   - Host Daemon launches AegisHub, Court Scribe, and initial Court personas

5. **System Verification**
   - `aegis status --json` (machine-readable output for tests)
   - `aegis verify` (optional deeper checks)

6. **Start First Conversation**
   - `aegis chat --headless "Hello, who are you?"`

## Testability Requirements (Critical for Implementers)
- All CLI commands must support `--headless` / `--json` flags for automation
- `aegis doctor` and `aegis status` must be fully deterministic and machine-parsable
- System startup must have a clear "ready" signal (e.g. specific log line or health endpoint)
- Integration test should be able to run the full journey end-to-end in CI
- Clear error messages and exit codes for each failure mode

## Security Touchpoints
- No secrets requested during install
- All binaries built from source (no pre-compiled downloads yet)
- Clear visibility into running components

## Related Documents
- (../../prd/user-experience-principles.md)
- (../../prd/user-personas.md)
- (../host-daemon.md)

## Open Questions
- Should there be a `make setup` target that runs doctor → build → start in one command?
- What should `aegis status` output look like exactly?