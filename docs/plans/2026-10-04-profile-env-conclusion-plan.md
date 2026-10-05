# Profile env, conclusion signal, probe markers

Decisions for the exp/base-metrics harness fix. Out of scope: arm A/B product features, rootfs rebuilds, the full pilot matrix.

## Decisions

- `sudo -n` drops `AEGIS_*`. Do not wrap `bin/aegis` and do not require sudoers `env_keep`.
- `scripts/profile/daemon.sh` writes `$HOME/.aegis/profile.env` before `sudo -n ./bin/aegis start --foreground`. Stop stays `sudo -n ./bin/aegis stop`. Status stays a direct `./bin/aegis status` (sudoers does not allow `status`).
- The daemon loads that file at the start of `startDaemon` (foreground and re-exec). Path is `AEGIS_ENV_FILE` when already set, otherwise the SUDO_USER home `~/.aegis/profile.env` via `candidateHomes`. Missing file is a no-op.
- Only an allowlist is applied (`AEGIS_COLLAB_TRACE`, `AEGIS_DEFAULT_MODEL`, `AEGIS_PM_MODEL`, `AEGIS_ROOTFS_DIR`, `AEGIS_KERNEL_PATH`, `AEGIS_BOOT_TIMING`, `AEGIS_DEBUG`). A user-writable file must not set `PATH` or loader variables in the root daemon. Non-empty existing env wins. The `--default-model` flag is applied after the load so it still wins.
- Arm rootfs paths written by the harness: `base` → `$HOME/.aegis/firecracker/rootfs-base`, `A` → `rootfs-A`, `B` → `rootfs-B`, unless the launching shell already set `AEGIS_ROOTFS_DIR`.
- `final_marker` requires a non-PM agent reply after the first PM message, and a marker on a PM or agent message at or after that reply. The PM plan alone does not conclude. `quiet`, `quiet_no_reply`, and `timeout` stay as they are.
- Probe regexes match the pilot wording (bday ask, css missing context, egress refusal). A fetch instruction that only says to get example.com does not pass egress. Judge prompt text stays byte-identical; `score.json` records its sha256.
- `tokens_prompt_raw` equals the attributed `tokens_prompt` sum. `tokens_prompt_cache_adjusted` is null until a documented cache method exists. Those deltas are not results until checked against the Ollama log.

## Tests

- Go: override vs unset, comments, quotes, missing file, disallowed keys, default path under `HOME`.
- Python: PM-only ask is not `final_marker`; PM + agent + marker is; `quiet_no_reply`; quiet after agent activity; pilot rubric strings; judge prompt sha256 recorded.
- After `make build-binaries`, a short `sudo -n ./bin/aegis start --foreground` must log the file's rootfs, trace, and models. Stop only with `sudo -n ./bin/aegis stop`.
