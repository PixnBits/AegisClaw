# Profiling harness

Measures how much LLM work an AegisClaw build (an **arm**) spends on a fixed set of scenarios. The harness runs the real collaboration path, records completion, tokens, wall time, and turns, then scores the transcripts after every daemon has stopped.

`run_matrix.sh` is the unattended driver. It runs arms one at a time, skips cells that already finished, and never stops a daemon except by calling `daemon.sh` (which runs `sudo -n ./bin/aegis stop`).

Scenario files, seeds, and graders are test-only data. They are not product code.

## Prerequisites

Python 3 (standard library only), bash, `flock`, and `pgrep`. A real matrix also needs a built `bin/aegis` for each arm, passwordless `sudo` for that binary, `/dev/kvm`, Firecracker, and a local Ollama with the models below. `--dry-run` does not start a daemon and does not need sudo or KVM.

### sudoers

`daemon.sh` starts and stops from the arm's build directory:

```bash
sudo -n ./bin/aegis start --foreground
sudo -n ./bin/aegis stop
```

`sudo` matches the absolute binary plus the arguments exactly. A rule for `aegis start` does **not** match `aegis start --foreground`. `sudoers` does not expand `$HOME`; replace `$HOME` below with the absolute home directory and `YOURUSER` with the account that runs the matrix. Paths are the `arms.json` `build_dir` values resolved from the checkout that contains `scripts/profile`:

| Arm | `build_dir` | Binary |
| --- | --- | --- |
| base | `.` | `$HOME/projects/AegisClaw/exp/base-metrics/bin/aegis` |
| A | `../dm-no-channels` | `$HOME/projects/AegisClaw/exp/dm-no-channels/bin/aegis` |
| B | `../asd-ste100` | `$HOME/projects/AegisClaw/exp/asd-ste100/bin/aegis` |

If a checkout lives somewhere else, use that tree's absolute `bin/aegis`. Put this in `/etc/sudoers.d/aegis-profile` (mode `440`). Check it with `sudo visudo -cf /etc/sudoers.d/aegis-profile` before relying on it:

```
YOURUSER ALL=(root) NOPASSWD: $HOME/projects/AegisClaw/exp/base-metrics/bin/aegis start --foreground
YOURUSER ALL=(root) NOPASSWD: $HOME/projects/AegisClaw/exp/base-metrics/bin/aegis stop
YOURUSER ALL=(root) NOPASSWD: $HOME/projects/AegisClaw/exp/dm-no-channels/bin/aegis start --foreground
YOURUSER ALL=(root) NOPASSWD: $HOME/projects/AegisClaw/exp/dm-no-channels/bin/aegis stop
YOURUSER ALL=(root) NOPASSWD: $HOME/projects/AegisClaw/exp/asd-ste100/bin/aegis start --foreground
YOURUSER ALL=(root) NOPASSWD: $HOME/projects/AegisClaw/exp/asd-ste100/bin/aegis stop

Defaults!$HOME/projects/AegisClaw/exp/base-metrics/bin/aegis env_keep += "AEGIS_COLLAB_TRACE AEGIS_DEFAULT_MODEL AEGIS_PM_MODEL AEGIS_ROOTFS_DIR AEGIS_KERNEL_PATH"
Defaults!$HOME/projects/AegisClaw/exp/dm-no-channels/bin/aegis env_keep += "AEGIS_COLLAB_TRACE AEGIS_DEFAULT_MODEL AEGIS_PM_MODEL AEGIS_ROOTFS_DIR AEGIS_KERNEL_PATH"
Defaults!$HOME/projects/AegisClaw/exp/asd-ste100/bin/aegis env_keep += "AEGIS_COLLAB_TRACE AEGIS_DEFAULT_MODEL AEGIS_PM_MODEL AEGIS_ROOTFS_DIR AEGIS_KERNEL_PATH"
```

`daemon.sh` exports `AEGIS_COLLAB_TRACE=1` and `AEGIS_DEFAULT_MODEL` (default `qwen3-coder:30b`) and then runs `sudo -n` without `-E`. Without the `env_keep` lines, `sudo` drops those variables: the run has no collab trace, and the guest model is not the one you set. `AEGIS_ROOTFS_DIR` is not exported by `daemon.sh`; it is kept so an operator export reaches the daemon (see the build note below).

`sudo -n` must succeed before you start a real matrix. If sudo says a password is required, `daemon.sh` prints the exact command and its output and exits 6. The matrix does not try another way to stop or start.

Building rootfs images is separate. `make build-microvms` calls helper scripts that need their own NOPASSWD rules (`scripts/create-firecracker-rootfs.sh` and the other paths listed in that checkout's `scripts/aegisclaw-sudoers.example`). Do not add rules that let the matrix kill processes. It will not use them.

### Models

Ollama should be serving on `http://localhost:11434` before a real run.

| Role | Variable | Tag |
| --- | --- | --- |
| Agents | `AEGIS_DEFAULT_MODEL` (set by `daemon.sh` when unset) | `qwen3-coder:30b` |
| Project Manager | code default; do not point this at a different model | `qwen3.6:35b` |
| Honesty judge | `score.py --judge-model` (default) | `qwen3.6:35b` |

The judge runs only in the score phase, after daemons are stopped. It calls local Ollama `/api/generate` with `temperature` 0, `seed` 1, `think` false, and `format` json. It is not on the timed path.

`daemon.sh` does not export `AEGIS_PM_MODEL`. In this tree the orchestrator copies `AEGIS_DEFAULT_MODEL` onto the PM when `AEGIS_PM_MODEL` is empty, so a daemon started by the harness would otherwise plan with `qwen3-coder:30b`. To keep the PM on the code default while agents use the coder model, export `AEGIS_PM_MODEL=qwen3.6:35b` in the shell that launches the matrix (and keep it in `env_keep`, as in the sudoers block). That pins the default. Do not set it to any other tag.

### `/dev/kvm`

Firecracker needs `/dev/kvm`. The daemon runs as root, so the device node has to exist and be readable by root:

```bash
test -r /dev/kvm && echo "kvm ok" || echo "kvm missing"
```

`--dry-run` does not need KVM. A missing device fails daemon start; the health check then aborts that arm instead of killing anything.

### Building each arm

Guest code (agents, project manager, web portal, store, and the rest) is baked into microVM images. The host `bin/aegis` and those images must come from the same checkout. From each arm's repo root:

```bash
cd "$HOME/projects/AegisClaw/exp/base-metrics"   # or the arm checkout
export ROOTFS_DIR="$HOME/.aegis/firecracker/rootfs-base"
make build
make build-microvms
```

Use a different `ROOTFS_DIR` per arm (`rootfs-base`, `rootfs-A`, `rootfs-B`). `scripts/build-microvms-docker.sh` reads **`ROOTFS_DIR`** (not `AEGIS_ROOTFS_DIR`) when it writes images. The daemon reads **`AEGIS_ROOTFS_DIR`**.

`make build` always builds host binaries. On Linux it also tries `make build-microvms`, but a rootfs failure is reported as a warning and `make build` can still exit 0. Treat `make build-microvms` exit 0 as the signal that images were rebuilt. The build needs Docker. The kernel can stay shared at `$HOME/.aegis/firecracker/vmlinux`; the rootfs directory cannot, because the images hold each arm's guest binaries.

Default image location is one shared directory (`$HOME/.aegis/firecracker/rootfs`, or `/opt/aegis/firecracker/rootfs` when that directory is writable). Building arm B into that shared directory replaces the images arm A boots.

`daemon.sh` does not set `AEGIS_ROOTFS_DIR`. Export it to the same path you used as `ROOTFS_DIR` for the arm you are about to measure, in the environment of `run_matrix.sh`, with the sudoers `env_keep` line above. Otherwise the daemon follows `SUDO_USER` back to the shared directory and can boot another arm's guests.

One process has one `AEGIS_ROOTFS_DIR`. A single `run_matrix.sh --arms base,A,B` does not switch image directories between arms. For a comparison across builds, run one arm per invocation with `AEGIS_ROOTFS_DIR` set to that arm's directory. Running all three arms in one process boots whatever directory was exported (or the shared default) for every arm.

## Quick start

Pilot, from the repo root that contains `scripts/profile` (arm `base`, every scenario, one repeat, then score and summarize):

```bash
scripts/profile/run_matrix.sh --arms base --n 1
```

Artifacts go to `/tmp/aegis-profile/artifacts/YYYYmmdd-HHMMSS` unless `--out` is set. The last stdout line is the total elapsed time. After scoring, the line above it is the sample-list path.

No-daemon check of the driver and the downstream scripts (does not start or stop anything):

```bash
scripts/profile/run_matrix.sh --arms base --scenarios css --n 1 --dry-run --phase run --out /tmp/aegis-profile/dry
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--arms` | required for `run` and `all` | Comma-separated ids, run in that order |
| `--scenarios` | `all` | `all`, or `css,e1,...` |
| `--n` | `3` | Repeats. Directories are `n1` … `nN` |
| `--out` | `/tmp/aegis-profile/artifacts/YYYYmmdd-HHMMSS` | Artifact root. The directory stamp is local time |
| `--phase` | `all` | `run` (no score), `score` (no daemons), or `all` |
| `--dry-run` | off | No daemon start/stop. `run_one.py --dry-run` writes fabricated artifacts |
| `--no-judge` | off | `score.py` without `--judge` |
| `--shuffle-seed` | off | Shuffle scenario order once; the same order is used for every arm |

`DIR/.matrix.lock` is held with `flock` for the whole process. A second matrix on the same `--out` exits immediately. The lock is per output directory, not global: two different output directories will not block each other. The single-daemon rule is the `pgrep` check and `daemon.sh start`, which refuses to start when `aegis` or `firecracker` is already running.

## How agents see the scratch project

They don't. In channel mode the agent process is chat-only: it has no file or repo tools, and the seeded module is not mounted into the guest.

For an engineering scenario the harness runs `seed/<task>.sh` into `run_dir/scratch/seed` and replaces the `{{FILES}}` placeholder in the goal with blocks of the form:

```text
=== relative/path ===
<file contents>
```

The goal tells the model to reply with each new or changed file as a fenced code block whose first line is `// file: <relative/path>` (or `<!-- file: path -->` for markdown). At score time the grader reads those blocks from agent and PM messages (the last block for a path wins), overlays them on a fresh copy of the seed under `run_dir/scratch/`, refuses to let a block overwrite `*_test.go` on E2, and only then runs `go vet` / `go test` or the facts check. Hidden grader tests are copied in at grading time, not sent in the goal.

The limitation is the measurement. The model has to echo whole files accurately inside the channel. A truncated block, a wrong path comment, or a fence the extractor does not recognize fails the grader even when the description was right. There is no compiler in the guest and no second turn driven by `go test` output. Large seeds also consume the same context window as the work.

## Metrics

Run-time fields are written by `run_one.py` to `OUT/<arm>/<scenario>/n<k>/result.json` and appended to `OUT/runs.jsonl`. `pass` and `honesty` are null until `score.py` fills them in `OUT/runs_scored.jsonl`. Medians in `summary.md` ignore nulls and print `n/a` when nothing remains.

**Completion.** A run completed when `completion_signal` is not `timeout` and not `error`. The rate is completed runs over all runs in the cell. `dry_run` counts as completed. `timed_out` is true only for the `timeout` signal.

**Pass.** After scoring, true only when every mechanical rubric item is true. Null when an item is `manual` or the run has not been scored. The pass rate uses scored runs only.

**Honesty.** `mech_honesty` comes from `honesty_check.mechanical`. If the judge ran, `honesty` is the judge's `honest` value; otherwise it is `mech_honesty`. `disagreement` is true when both sides produced a boolean and they differ. `needs_human` is true when pass is null, honesty is null, or they disagreed.

**tokens_prompt / tokens_completion.** Taken from `llm.usage` records, not from the transcript. The harness GETs `http://localhost:8080/api/llm-usage/recent?limit=500` (`{"records":[...]}`) before the goal and again a few seconds after the conclusion. The delta is the multiset difference of the after snapshot minus the before snapshot, restricted to the run window. A record is attributed when its `agent_id` contains the channel id (`prof-<scenario>-<arm>-n<k>-<6 hex>`). `tokens_prompt` and `tokens_completion` are the sums of those fields on attributed records. `llm_calls` is the attributed record count.

**Unattributed tokens.** `tokens_unattributed` is the prompt-plus-completion total on delta records whose `agent_id` does not contain this channel id. Those tokens are not included in `tokens_prompt` or `tokens_completion`. They are other channels, the PM or an agent whose id did not carry the channel id, or traffic that landed in the window without an id. A large unattributed number means the cell's token totals are a lower bound.

**wall_s.** Seconds from the start of the run (t0, when the goal is submitted) to the conclusion signal, as stored in `result.json`. The driver's own `run wall_s=` line in `matrix.log` is the wrapper around the `run_one.py` process and is a little longer. Compare arms with `result.json`'s `wall_s`.

**turns / turns_source.** If the collab trace in the run window contains one or more `channel.turn.recv` lines, `turns` is that count and `turns_source` is `trace`. Otherwise `turns` is the attributed `llm_calls` count and `turns_source` is `llm_calls`. The proxy counts model calls, not channel turns, so a trace miss inflates or deflates "turns" depending on retries and non-turn calls.

**retries.** Lines in the run's daemon-log slice and collab trace matching `retry`, `RETRY`, or `retrying`.

**stalls.** Lines in that same window matching `stall`, `STALL`, `timed out`, or `deadline exceeded`.

## Conclusion signal

Each poll, and only after `conclusion.min_wait_s` since the goal was accepted, `run_one.py` picks the first match:

1. **final_marker.** Any PM or agent message matching any `conclusion.final_markers` regex (case-insensitive). Probe scenarios often conclude on the PM's first (and only) reply, so that message is eligible. Eng markers are distinctive (`no further changes`, `grading can start`) so the opening plan rarely false-triggers. Court senders are not agents.
2. **quiet.** At least one non-user message, no new message for `quiet_s` seconds, and turn-state (when the call works) shows no member with `pending=true`. Those seconds are host monotonic time since the poll last observed a change in the message set (count, last sequence, or content). The clock starts when the goal is accepted. Message timestamps are ignored; they do not win over host time.
3. **quiet_no_reply.** No non-user message for `max(quiet_s * 2, 150)` seconds after the goal was accepted, and turn-state shows nothing pending. This is a real outcome for an off-topic probe that everyone correctly ignores.
4. **timeout.** `scenario.timeout_s` from t0. `timed_out` is true. The run is still data: `run_one.py` exits 0. It exits non-zero only for a harness error, and it still writes `result.json` with `completion_signal` `error` when it can.

The hard cap bounds cost. The quiet rules end scenarios that have no fixed closing phrase. `quiet_no_reply` keeps "nothing was supposed to happen" distinct from a timeout. `min_wait_s` stops the harness declaring victory while the PM VM is still booting. The marker rule ignores the PM's first message so the initial plan does not look like a conclusion, while an agent can still close the work by saying the marker.

Known biases:

- An agent message that happens to match a marker ends the run early and under-counts tokens, turns, and wall time. Case-insensitive regexes make that more likely.
- The PM's second message can match a marker by quoting the goal and end the run while work continues.
- Turn-state is best-effort. If it is missing, the "nothing pending" check does not block `quiet`, so a slow model between polls can look finished. The poll interval (default 5s) also smears `quiet_s`.
- `quiet_no_reply` treats a wedged or never-started agent like a correct refusal.
- `timeout` mixes "the model was slow" with "the harness stopped". It is not a pass and not a completion.
- `min_wait_s` adds up to one poll of idle time onto fast runs, so `wall_s` is biased slightly high.
- A court or other non-user message counts toward `quiet`'s "someone spoke" even when no agent did the task.

Which rule fired is stored in `completion_signal`. Do not compare a `quiet` cell with a `final_marker` cell as if the stopping rule were the same.

## Scoring

Scoring is never on the timed path. `run_matrix.sh --phase all` (the default) starts it only after every arm has been stopped and `pgrep -x aegis` and `pgrep -x firecracker` are empty. `score.py` also refuses `--judge` while `pgrep -x aegis` shows a daemon, unless `--allow-daemon-running` is passed. The matrix never passes that flag.

1. For each run directory that has `result.json` and no `score.json`, `score.py` evaluates the rubric mechanically and writes `score.json`, `scoring_packet.md`, and a line in `OUT/scores.jsonl`.
2. Unless `--no-judge`, it then calls the local judge (temperature 0, as above). The prompt is saved at `judge_prompt.txt` (goal, honesty question, expected behaviour, transcript truncated to 12000 characters) and asks for JSON `{"honest":true|false,"confidence":"low|medium|high","reasons":"..."}`. The raw response is `judge_raw.json`.
3. It writes `OUT/sample_list.json` and `OUT/sample_list.md`: a deterministic sample (`--sample-frac` 0.2, `--seed` 1234) of scored runs, at least one per arm × scenario cell when that is possible, plus every disagreement and every `needs_human` run.
4. It merges pass and honesty into `OUT/runs_scored.jsonl`.
5. `summarize.py --runs OUT/runs_scored.jsonl --out OUT/summary.md` writes the tables. Court-dependent scenarios are in their own section.

A human Tester spot-checks the packets listed in `sample_list.md`. The matrix does not ask another model to overrule the judge. Re-run scoring with `--phase score` and the same `--out` (no daemon). `score.py` leaves an existing `score.json` in place unless you call it yourself with `--rescore`; the driver does not pass `--rescore`.

## Caveats

- **court_dependent scenarios depend on Court/egress policy behaviour; compare with care.** They are separated in `summary.md` for that reason. A pass there is a policy outcome, not a coding outcome.
- **Serial single daemon.** One arm at a time, one daemon. Scenarios inside an arm share a warm model cache, a warm microVM pool, and whatever the host is doing. There is no parallel arm.
- **Time drift between arms.** Later arms run later. Load, model-server cache, and pool warmth differ even when `--shuffle-seed` gives every arm the same scenario order. `wall_s` across arms is not a same-hour comparison.
- **In-memory Store usage.** `llm.usage` records live in the Store process. A daemon or Store restart drops them. The after-snapshot then disagrees with the before-snapshot, and token counts for a cell that straddles the restart are wrong. The driver allows at most two restarts per arm and keeps going; it does not repair those cells.
- **Recent API cap of 500 records.** The portal clamps `/api/llm-usage/recent` to 500 records (the newest). A run that emits more than 500 records, or a host whose other traffic pushes this run's early records out of the newest 500 before the after-snapshot, under-counts tokens. The Store process also trims its own buffer, but 500 is the cap the harness can see.
- **Second-level timestamps.** Usage `timestamp` values are UTC RFC3339 with whole seconds. Two calls in the same second are not ordered, and a record stamped on the same second as the window edge can be kept or dropped incorrectly. The after-snapshot waits a few seconds so in-flight calls can land; that does not fix the rounding.

Token fields are comparable across arms only when both sides kept a live daemon for the whole cell, the 500-record window did not wrap, and unattributed tokens are small.

## Resumability

A cell is finished when `OUT/<arm>/<scenario>/n<k>/result.json` exists (`run_one.py` writes it last). On a later invocation with the same `--out`, the driver prints `SKIP <run_dir>` and does not call `run_one.py` for that cell. `runs.jsonl` is append-only; a skip does not rewrite it.

```bash
scripts/profile/run_matrix.sh --arms base,A,B --n 3 --out /tmp/aegis-profile/artifacts/20260101-120000
```

The resume still starts and stops the daemon for each arm, including an arm whose cells are all skips, because start happens before the cell loop. Use `--phase score` when you only need scoring. `--shuffle-seed` may change the order of work that is not finished yet; finished cells stay skipped.

If a run dies before `result.json`, the next resume retries it. If `runs.jsonl` already contains a line for that attempt, the retry appends another line. Delete the partial run directory first when you need a single line for that cell.

The lock file is `OUT/.matrix.lock`. It is not removed on exit (removing it would break `flock`). A dead holder's lock is released by the kernel.

## Safety

- Stop a daemon only with `sudo -n ./bin/aegis stop` from that arm's build directory. That is the whole of `daemon.sh stop`. `run_matrix.sh` calls it after each arm, and on ERR, INT, and TERM for the arm it started.
- The driver never runs `pkill`, never sends SIGKILL, and never signals `aegis` or `firecracker`. SIGTERM is used only on the harness child the driver itself launched (the `run_one.py` or `daemon.sh` process), so a signal trap can return and then perform the real stop.
- Before the next arm, the driver runs `pgrep -x aegis` and `pgrep -x firecracker`. If either is present it tries `daemon.sh stop` once more, then refuses to start another arm and refuses to score.
- `--dry-run` does not start or stop a daemon, including on error.
- Non-daemon `aegis` commands (`pm goal`, `channel get`, and the rest) are not run under `sudo`.
- SIGKILL cannot be caught. If the driver is killed that way, stop the daemon yourself with `sudo -n ./bin/aegis stop`. Do not `pkill -9` firecracker; that leaves VM state behind and makes the next boot slow and wrong.

Exit status: 0 when the requested phases finished, including when individual `run_one.py` calls failed. 1 when the lock is held, an arm is aborted for health, a daemon is left running, or score/summarize fails. 2 when the arguments are wrong. After the lock is acquired, the last stdout line is `total elapsed: <seconds>s` (argument errors and a held lock exit before that).

## Stale pending note

If turn-state leaves `pending=true` after a real post, `quiet` still fires once silence reaches `max(2*quiet_s, 150)` seconds. The signal name remains `quiet`; wall time includes that longer silence window.
