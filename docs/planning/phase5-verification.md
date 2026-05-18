# Phase 5: Verification, Measurement & Completion

## 1. Automated Tests

Basic verification tests added in `cmd/aegisclaw/daemon_test.go` covering:
- Static binary expectation
- Lifecycle containment presence
- Minimal privilege
- No secret handling policy

Full enforcement of memory (<20MB) and static binary is best done in CI.

## 2. Re-measurement

### Lines of Code
Run:
```bash
find cmd/aegisclaw internal -name '*.go' | xargs wc -l | tail -1
```

### Idle Memory
```bash
./aegisclaw &
sleep 2
ps -o pid,rss,command -p $!
```

Target: < 20 MB RSS idle.

## 3. Final Forbidden Pattern Review

Recommended grep commands:
```bash
# Business logic
rg -i "proposal|memory|eventbus|chat|session|worker" cmd/aegisclaw/*.go

# Secret handling
rg -i "secret|password|token|private_key" cmd/aegisclaw/*.go

# Governance
rg -i "court|vote|approve|reject" cmd/aegisclaw/*.go
```
