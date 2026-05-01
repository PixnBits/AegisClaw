# first_skill_tutorial_live_test.go — cmd/aegisclaw

## Purpose
Live variant of the first-skill tutorial journey test. Requires Firecracker, KVM, root privileges, and an `alpine.ext4` rootfs template — not available in standard CI. Excluded from `go test ./...` by the `livetest` build tag.

## Running
```bash
sudo ./scripts/run-live-test.sh
# To regenerate cassette:
RECORD_OLLAMA=true sudo ./scripts/run-live-test.sh
```

## System Fit
Most faithful test of the SDLC flow end-to-end. The live-test CI job explicitly marks this as "Skipped" on pull requests to signal it requires special infrastructure.

## Notable Dependencies
- Build tag: `livetest`
- Requires `/dev/kvm`, root, Firecracker binary, and `alpine.ext4`.
