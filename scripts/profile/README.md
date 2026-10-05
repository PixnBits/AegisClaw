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
```

`sudo -n` resets the environment. It does not keep `AEGIS_*`. This harness does not use sudoers `env_keep` and does not wrap `bin/aegis`. Before `sudo -n ./bin/aegis start --foreground`, `daemon.sh` writes `$HOME/.aegis/profile.env` in the invoking user's home (not `/root`). The daemon, running as root, resolves that same path from `SUDO_USER` and loads keys that are unset or empty. A key that is already set in the process is never overwritten. `AEGIS_ENV_FILE` selects a different file only when it is already in the daemon's environment. Do not rely on passing it through `sudo`; the default path is how root finds the file.

The file written on start is:

```
AEGIS_COLLAB_TRACE=1
AEGIS_DEFAULT_MODEL=qwen3-coder:30b
AEGIS_PM_MODEL=qwen3.6:35b
AEGIS_ROOTFS_DIR=<absolute arm rootfs directory>
```

Shell values of `AEGIS_DEFAULT_MODEL` and `AEGIS_PM_MODEL`, when set, are what gets written. `AEGIS_KERNEL_PATH`, `AEGIS_BOOT_TIMING`, and `AEGIS_DEBUG` are written when that shell set them. Other existing `AEGIS_*` lines in the file are kept. The daemon's allowlist is those seven keys; other lines in the file are ignored.

`AEGIS_ROOTFS_DIR` defaults by arm, unless the shell that launches `daemon.sh` already exported `AEGIS_ROOTFS_DIR` (that absolute path is copied into the file):

| Arm | `AEGIS_ROOTFS_DIR` |
| --- | --- |
| base | `$HOME/.aegis/firecracker/rootfs-base` |
| A | `$HOME/.aegis/firecracker/rootfs-A` |
| B | `$HOME/.aegis/firecracker/rootfs-B` |

Each `daemon.sh start` rewrites the file for that arm, so a matrix that runs base, then A, then B switches image directories between arms. Build each arm's images into the directory in the table (`make build-microvms` reads `ROOTFS_DIR`, not `AEGIS_ROOTFS_DIR`).

`sudo -n` must succeed before you start a real matrix. If sudo says a password is required, `daemon.sh` prints the exact command and its output and exits 6. The matrix does not try another way to stop or start. Stop is only `sudo -n ./bin/aegis stop`. Status is `./bin/aegis status` (not sudo).

Building rootfs images is separate. `make build-microvms` calls helper scripts that need their own NOPASSWD rules (`scripts/create-firecracker-rootfs.sh` and the other paths listed in that checkout's `scripts/aegisclaw-sudoers.example`). Do not add rules that let the matrix kill processes. It will not use them.

### Models

Ollama should be serving on `http://localhost:11434` before a real run.

| Role | Variable | Tag |
| --- | --- | --- |
| Agents | `AEGIS_DEFAULT_MODEL` in `~/.aegis/profile.env` | `qwen3-coder:30b` |
| Project Manager | `AEGIS_PM_MODEL` in `~/.aegis/profile.env` | `qwen3.6:35b` |
| Honesty judge | `score.py --judge-model` (default) | `qwen3.6:35b` |

The judge runs only in the score phase, after daemons are stopped. It calls local Ollama `/api/generate` with `temperature` 0, `seed` 1, `think` false, and `format` json. It is not on the timed path.

`daemon.sh` writes both model tags into `profile.env` on start. The PM tag stays `qwen3.6:35b` so the orchestrator does not copy the coder model onto the PM. Do not point `AEGIS_PM_MODEL` at another tag.

After `wait_ready`, `daemon.sh start` prewarms both tags so a PM call on `qwen3.6:35b` does not leave the agent model cold for the next turn. For each of `AEGIS_DEFAULT_MODEL` (default `qwen3-coder:30b`) and `AEGIS_PM_MODEL` (default `qwen3.6:35b`) it POSTs `http://127.0.0.1:11434/api/generate` with `{"model":"...","prompt":"ping","stream":false,"keep_alive":"60m"}` (`curl -sS --max-time 600`). It logs `prewarm <model> ok` or `prewarm <model> fail`. A prewarm failure does not fail start; the daemon is already ready. See also "Run isolation".

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

Use a different `ROOTFS_DIR` per arm (`rootfs-base`, `rootfs-A`, `rootfs-B`, as in the table under sudoers). `scripts/build-microvms-docker.sh` reads **`ROOTFS_DIR`** (not `AEGIS_ROOTFS_DIR`) when it writes images. The daemon reads **`AEGIS_ROOTFS_DIR`** from `~/.aegis/profile.env`, which `daemon.sh` fills with that arm's absolute directory on each start.

`make build` always builds host binaries. On Linux it also tries `make build-microvms`, but a rootfs failure is reported as a warning and `make build` can still exit 0. Treat `make build-microvms` exit 0 as the signal that images were rebuilt. The build needs Docker. The kernel can stay shared at `$HOME/.aegis/firecracker/vmlinux`; the rootfs directory cannot, because the images hold each arm's guest binaries.

The shared directory (`$HOME/.aegis/firecracker/rootfs`, or `/opt/aegis/firecracker/rootfs`) is the daemon's fallback when `profile.env` does not set `AEGIS_ROOTFS_DIR`. Do not build every arm into that shared directory: the next arm replaces the images. `daemon.sh start` writes the per-arm path, so `run_matrix.sh --arms base,A,B` switches directories between arms as long as each arm was built into its own `rootfs-*` directory.

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
| `--arms` | required for `run` and `all` | Comma-separated ids, run in that order. `dm` is arm A and `ste` is arm B, so `base,dm,ste` is `base,A,B` |
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

**e1 / e3 chat-only delivery.** Engineering scenarios may end on `quiet` when agents discuss the work in the channel but never post a `// file:` fence (markdown may use `<!-- file: path -->`). The mechanical grader then fails, because it only sees files inside those fences. `final_marker` does not fire on the PM's opening plan, so a conversation that never delivers a file waits out `quiet` or `timeout` unless a later PM post matches a marker. That is a known product and harness limitation, not a grader bug. e3's markers also include the file fence and the completion phrases (`no further changes`, `grading can start`). An agent reply whose stripped text contains `?` and no code fence or `// file:` does not conclude, even when it contains one of those phrases.

## Metrics

Run-time fields are written by `run_one.py` to `OUT/<arm>/<scenario>/n<k>/result.json` and appended to `OUT/runs.jsonl`. `pass` and `honesty` are null until `score.py` fills them in `OUT/runs_scored.jsonl`. Medians in `summary.md` ignore nulls and print `n/a` when nothing remains.

**Turn.** A turn is one LLM-bearing agent or PM response that produces an outbound user-visible message (a channel post or a DM `chat.message` counted in the transcript). Count a DM and a channel post the same: one such message is one turn, on every arm. The user goal is not a turn. A Court message is not a turn. A facilitator `system` status post is not a turn, not an agent message, and not the agent reply that makes `final_marker` eligible. When the harness collected a transcript, `turns` is that count and `turns_source` is `transcript`. The same count is `pm_messages + agent_messages`. If no transcript was collected, `turns` falls back to `channel.turn.recv` lines in the collab trace (`turns_source` `trace`) or, when those lines are absent, to attributed `llm_calls` (`turns_source` `llm_calls`). Those fallbacks are not the cross-arm definition. Do not compare them with `transcript` turns. Rows from earlier pilots stored the trace count in `turns`.

**Completion.** A run completed when `completion_signal` is not `timeout` and not `error`. The rate is completed runs over all runs in the cell. `dry_run` counts as completed. `timed_out` is true only for the `timeout` signal.

**Pass.** After scoring, true only when every mechanical rubric item is true. Null when an item is `manual` or the run has not been scored. The pass rate uses scored runs only.

**Honesty.** `mech_honesty` comes from `honesty_check.mechanical`. If the judge ran, `honesty` is the judge's `honest` value; otherwise it is `mech_honesty`. `disagreement` is true when both sides produced a boolean and they differ. `needs_human` is true when pass is null, honesty is null, or they disagreed.

**tokens_prompt / tokens_completion.** Taken from `llm.usage` records, not from the transcript. The harness GETs `http://localhost:8080/api/llm-usage/recent?limit=500` (`{"records":[...]}`) before the goal and again a few seconds after the conclusion. The delta is the multiset difference of the after snapshot minus the before snapshot, restricted to the run window. A record is attributed when its `agent_id` contains the channel id (`prof-<scenario>-<arm>-n<k>-<6 hex>`). `tokens_prompt` and `tokens_completion` are the sums of those fields on attributed records. `llm_calls` is the attributed record count. What those sums mean (full-prompt API vs journal cache-adjusted) is in "Token accounting".

**Unattributed tokens.** `tokens_unattributed` is the prompt-plus-completion total on delta records whose `agent_id` does not contain this channel id. `tokens_prompt_unattributed` and `tokens_completion_unattributed` are the two sums. Those tokens are not included in `tokens_prompt` or `tokens_completion`. They are other channels, the PM or an agent whose id did not carry the channel id, or traffic that landed in the window without an id. A large unattributed number means the cell's token totals are a lower bound.

**wall_s.** Seconds from the start of the run (t0, when the goal is submitted) to the conclusion signal, as stored in `result.json`. It does not include the drain. **drain_s** is seconds spent polling after that signal before the final usage snapshot (0 on a dry-run, a harness error, or an error before the goal was accepted). **total_s** is `wall_s + drain_s`. Compare conclusion time with `wall_s`. Token and journal windows use `total_s`. The driver's own `run wall_s=` line in `matrix.log` is the wrapper around the `run_one.py` process and is a little longer.

**retries.** Lines in the run's daemon-log slice and collab trace matching `retry`, `RETRY`, or `retrying`.

**stalls.** Lines in that same window matching `stall`, `STALL`, `timed out`, or `deadline exceeded`.

## Token accounting

Checked on this host with Ollama 0.33.2 (repeatable) and against the pilot1/pilot2 daemon logs. This is the definition of the token fields. It is not a license to publish token deltas.

**Raw vs cache-adjusted.** The network boundary copies Ollama `prompt_eval_count` into `llm.usage.record` as `tokens_prompt`, and `eval_count` as `tokens_completion`. The harness sums the usage-API delta. It does not recompute tokens from the transcript.

- `tokens_prompt` / `tokens_prompt_raw` are the attributed sum of API `prompt_eval_count`. On Ollama 0.33.2 that count is the **full prompt**, not reduced by KV cache. Sending the same 1469-token prefix twice to `qwen3-coder:30b` returned `prompt_eval_count=1469` both times.
- `tokens_prompt_cache_adjusted` is the journal **newly evaluated** prompt-token sum: the `N` in `prompt eval time = ... / N tokens`. For that second 1469-token call the journal said `cached n_tokens = 1457` and `prompt eval time = 53.33 ms / 12 tokens`. `qwen3.6:35b` (hybrid/SWA) logs `forcing full prompt re-processing` and the journal N equals the full prompt.
- After a live run (not `--dry-run`) the harness reads `journalctl -u ollama -o short-iso --since ... --until ... --no-pager` for the run window. The window length is `total_s` (conclusion `wall_s` plus drain), not `wall_s` alone, so a call that finishes during the drain is counted. On success with timing lines it sets `tokens_prompt_cache_adjusted`, `journal_llm_calls` (number of prompt-eval lines), and `tokens_cache_method` `api_prompt_eval_count_full__journal_prompt_eval_new`. If journalctl is missing or the excerpt has no timing lines, cache-adjusted is null and the method is `api_prompt_eval_count_full__journal_unavailable`. A journal failure does not fail the run.
- Do not subtract a cache-similarity fraction from `prompt_eval_count`. Do not treat a journal prompt sum below API raw as a dropped record.

**Attribution.** A usage record is attributed when its `agent_id` contains the channel id, and its timestamp (when present) falls in `[started_at, started_at + total_s]` plus the short settle used for the after-snapshot. `total_s` is conclusion `wall_s` plus drain, so a call that finishes during the drain still counts for this run. `llm_calls` is that attributed record count. Court (and any other record whose `agent_id` does not contain the channel id) is unattributed: `tokens_unattributed`, `tokens_prompt_unattributed`, `tokens_completion_unattributed`. Those tokens are not included in `tokens_prompt` or `tokens_completion`. A large unattributed number means the cell's attributed totals are a lower bound.

**Repeat the verification.** For one finished run directory:

```bash
python3 scripts/profile/check_ollama_journal.py RUN_DIR --run-journalctl
```

Or save the window and compare:

```bash
journalctl -u ollama -o short-iso --since '...' --until '...' --no-pager > /tmp/ollama-window.log
python3 scripts/profile/check_ollama_journal.py RUN_DIR --journal /tmp/ollama-window.log
```

The script sums journal prompt-eval N as cache-adjusted and journal eval M as completion. It compares completion sum and prompt-eval line count to the result. A journal prompt sum below API raw is a cache hit and is not a failure. The window stops before a later `score.py --judge` generate. The script does not change `result.json`.

**Pilot1/pilot2 single-call runs.** Token capture was correct; agents never ran. In every pilot run the PM posted its plan with `@Coder`/`@CISO`, then sent `ensure.role` **after** `channel.post`. The facilitator scheduled turns for the plan while channel members were only `[project-manager]`, skipped that post as `self_post`, and the Coder/CISO VMs joined about a second later with no further `channel.updated` to trigger a turn. Result: exactly one LLM call (the PM plan) per run. The fix is to send `ensure.role` (and the CISO `channel.add_member` block) **before** the plan `channel.post`, so those roles are members when the facilitator schedules turns for the plan.

Do not present token deltas as experiment results until the same journal check has been done on a multi-call run that actually includes a PM turn, an agent turn, and a Court turn. A PM-only match does not license comparing arms on tokens.

## Conclusion signal

Each poll, and only after `conclusion.min_wait_s` since the goal was accepted, `run_one.py` picks the first match:

1. **final_marker.** A `conclusion.final_markers` regex (case-insensitive) matches in either of two ways. A PM message after the first PM post (a later PM post, not the opening plan) may match on its own, even when every agent reply so far is only a clarifying question. Otherwise the marker needs a non-clarifying agent reply after the first PM, then a match on a PM or non-clarifying agent message at or after that reply. An agent reply is clarifying when its stripped text contains `?` and has no concrete deliverable marker. Deliverable markers are case-sensitive: a markdown code fence opener (three backticks) or the substring `// file:`. That reply does not make the marker eligible, and a marker phrase inside it does not fire `final_marker`. The PM's first plan still does not count. Court senders and facilitator `system` status posts are not agents and do not count as the required reply. If the only agent replies are clarifying and no later PM post matches, `quiet` and `timeout` are the fallbacks.
2. **quiet.** At least one non-user message, no new message for `quiet_s` seconds, and turn-state (when the call works) shows no member with `pending=true`. Those seconds are host monotonic time since the poll last observed a change in the message set (count, last sequence, or content). The clock starts when the goal is accepted. Message timestamps are ignored; they do not win over host time.
3. **quiet_no_reply.** No non-user message for `max(quiet_s * 2, 150)` seconds after the goal was accepted, and turn-state shows nothing pending. This is a real outcome for an off-topic probe that everyone correctly ignores.
4. **timeout.** `scenario.timeout_s` from t0. `timed_out` is true. The run is still data: `run_one.py` exits 0. It exits non-zero only for a harness error, and it still writes `result.json` with `completion_signal` `error` when it can.

The hard cap bounds cost. The quiet rules end scenarios that have no fixed closing phrase. `quiet_no_reply` keeps "nothing was supposed to happen" distinct from a timeout: nobody non-user has replied and turn-state shows nothing pending. `min_wait_s` stops the harness declaring victory while the PM VM is still booting. The marker rule does not treat the PM's opening plan as a conclusion. A probe that only gets a clarifying ask (the PM's question, or an agent reply whose stripped text contains `?` and no deliverable marker) ends on `quiet` (someone spoke) or `timeout`, not `final_marker`, unless a later PM post matches a marker. An agent can still close the work by saying the marker in a reply that is not clarifying (no `?`, or a `?` together with a code fence or `// file:`).

**Drain.** After the signal is chosen (`final_marker`, `quiet`, `quiet_no_reply`, `timeout`, or `error` once the goal was accepted), the harness keeps polling before the final transcript read and the usage snapshot. Drain ends only when all three are true: turn-state shows no pending member, or turn-state is unavailable (that does not block, so a quiet channel can still finish); no new attributed usage record for this channel since the previous drain observation (`agent_id` contains the channel id); and the message signature is unchanged for `DRAIN_QUIET_S` (15 seconds). The cap is `DRAIN_CAP_S` (120 seconds) from the start of the drain. `wall_s` stops at the conclusion signal. `drain_s` is the time spent draining. `total_s` is `wall_s + drain_s`. The journal window uses `total_s`, so a call that lands during the drain counts for this run. An error before the goal is accepted does not drain (`drain_s` 0, `total_s` equal to `wall_s`).

Known biases:

- A non-clarifying agent message that happens to match a marker ends the run early. `wall_s` stops at that signal, so wall time is short; the drain still waits for in-flight usage, and those calls count in `total_s`. An agent reply that contains `?` and no deliverable marker does not fire the marker. Case-insensitive regexes make an early match more likely.
- The PM's second message can match a marker by quoting the goal and end the run while work continues.
- Turn-state is best-effort. If it is missing, the "nothing pending" check does not block `quiet`, so a slow model between polls can look finished. The poll interval (default 5s) also smears `quiet_s`.
- `quiet_no_reply` treats a wedged or never-started agent like a correct refusal.
- `timeout` mixes "the model was slow" with "the harness stopped". It is not a pass and not a completion.
- `min_wait_s` adds up to one poll of idle time onto fast runs, so `wall_s` is biased slightly high.
- A court or other non-user message counts toward `quiet`'s "someone spoke" even when no agent did the task.

Which rule fired is stored in `completion_signal`. Do not compare a `quiet` cell with a `final_marker` cell as if the stopping rule were the same.

## Run isolation

No channel close or archive command exists. Between finished scenario cells in one arm, except the last cell and except `--dry-run`, `run_matrix.sh` restarts the daemon: `daemon.sh stop`, then `daemon.sh start`. The log line is `isolate restart arm=... stop_s=... start_s=... total_s=...`. On validate3 that cost was about 36 seconds (ready ~5s + stop ~31s), under a 60 second budget. These restarts are not health-failure restarts and do not increment the per-arm restart cap (2). After the last cell, `finish_arm` stops the daemon and does not start it again.

Usage attribution still keeps a record only when `agent_id` contains that run's channel id. The journal window is `total_s` (conclusion `wall_s` plus drain), so a call captured during drain counts for that run only and not for the next one. The isolate restart runs after that cell's usage snapshot. The next cell's before-snapshot is a fresh store. Arms also prewarm both models with `keep_alive` 60m after the daemon is ready (see "Models") so the gap between the PM's `qwen3.6:35b` turn and the first agent turn is not a cold load.

## Arm A (DM, Court-out)

Arm A (`arms.json` id `A`, alias `dm`, `build_dir` `../dm-no-channels`) collaborates by PM-to-agent DMs. The final PM synthesis may be a single channel post. Start the matrix from this tree. `run_one.py` runs that checkout's `bin/aegis`.

Arm A is Court-out. `court_dependent` scenarios stay in their own section of `summary.md`. Compare those rows separately. Do not treat an Arm A egress failure as a collaboration-efficiency signal. It is the missing Court or policy path, not a measure of how much DM work the arm did.

`final_marker` is the same rule as every other arm. It fires when a later PM message matches a marker, or when a marker matches a PM or non-clarifying agent message at or after the first agent reply that is not clarifying (stripped text contains `?` and has no code fence or `// file:`). On arm A that agent message may be a DM-mirrored channel post (sender classifies as `agent`, or the dump sets `role` to `agent`) or a row merged from the DM transcript. The PM's opening plan alone still does not conclude. A clarifying agent question does not.

Arm A sets `"messaging": "dm"`. A scenario file may set `"messaging"` to `dm` or `channel` and override the arm. Each poll still runs `channel get`. When messaging is `dm`, the harness also merges DM messages that the product exposes, and it does not fail the run when a dump is missing:

1. JSON files. The conventional path is `<arm build_dir>/scripts/profile/dm-dump/<channel>.json`. Arm A also lists that path as `dm_files`. The arm may add more paths in `dm_files` or in `<arm build_dir>/scripts/profile/harness_dm.json`. A relative path is inside the build directory. An absolute path must stay inside it. `{channel}` is replaced with the channel id. A path that leaves the build directory is ignored.
2. A CLI dump. `dm_cli` in `arms.json`, or `cli` in `harness_dm.json`, is a list of argument lists (or one argument list). `{channel}` is replaced. When neither file documents a CLI, the harness tries `dm dump --json <channel>` and `chat dump --json <channel>`, then remembers an unknown command and does not try it again. Any other failure, timeout, or non-transcript output is skipped.

`harness_dm.json` shape, written by the arm checkout when it wants a different dump:

```json
{
  "cli": [["dm", "dump", "--json", "{channel}"]],
  "files": ["scripts/profile/dm-dump/{channel}.json"]
}
```

A message already on the channel with the same role, sender, and content is not added again. When every kept message has a timestamp, order is that timestamp. Otherwise undated DM rows are placed so a later channel PM synthesis stays after the DM plan and the agent replies. `transcript.json` for these runs is the merged list `{"messages":[...]}`. Scoring reads that list. The judge prompt text is not changed. An explicit `role` of `user`, `pm`, `court`, `agent`, or `system` is that role. Any other `role` string is ignored and the sender name is classified as before (`project-manager…` is PM, `court…` is Court, sender `system` is the facilitator, anything else that is not the user is an agent).

## Scoring

Scoring is never on the timed path. `run_matrix.sh --phase all` (the default) starts it only after every arm has been stopped and `pgrep -x aegis` and `pgrep -x firecracker` are empty. `score.py` also refuses `--judge` while `pgrep -x aegis` shows a daemon, unless `--allow-daemon-running` is passed. The matrix never passes that flag.

1. For each run directory that has `result.json` and no `score.json`, `score.py` evaluates the rubric mechanically and writes `score.json`, `scoring_packet.md`, and a line in `OUT/scores.jsonl`.
2. Unless `--no-judge`, it then calls the local judge (temperature 0, as above). The prompt is saved at `judge_prompt.txt` (goal, honesty question, expected behaviour, transcript truncated to 12000 characters) and asks for JSON `{"honest":true|false,"confidence":"low|medium|high","reasons":"..."}`. The raw response is `judge_raw.json`. The prompt text is fixed. `score.json` records `judge_prompt_sha256`, the SHA-256 of that prompt (the text that would be sent, even when `--judge` is off, or the bytes written to `judge_prompt.txt` when the judge ran).
3. It writes `OUT/sample_list.json` and `OUT/sample_list.md`: a deterministic sample (`--sample-frac` 0.2, `--seed` 1234) of scored runs, at least one per arm × scenario cell when that is possible, plus every disagreement and every `needs_human` run.
4. It merges pass and honesty into `OUT/runs_scored.jsonl`.
5. `summarize.py --runs OUT/runs_scored.jsonl --out OUT/summary.md` writes the tables. Court-dependent scenarios are in their own section.

A human Tester spot-checks the packets listed in `sample_list.md`. The matrix does not ask another model to overrule the judge. Re-run scoring with `--phase score` and the same `--out` (no daemon). `score.py` leaves an existing `score.json` in place unless you call it yourself with `--rescore`; the driver does not pass `--rescore`.

## Deviations from main

Exp-only changes are listed in `docs/exp/base-metrics-deviations.md`. They are not on main.

- ACL: `daemon-internal*` and `daemon-internal-*` → store include `llm.*` and `llm.usage.*`. The portal usage API is host-bridged as `daemon-internal`; without that grant, `/api/llm-usage` and `/api/llm-usage/recent` return empty.
- Hub: `deliverPendingRPC` accepts a `permission.snapshot` reply when the waiter requested that command. Store's RPC reply reuses the unsolicited-push name; without the match, `hub-perm-fetch-*` times out and VMs are pushed snapshot v0.
- Permissions: `channel.turn_result` and `channel.add_member` are ACL-only (also `channel.turn` and `channel.member_turn_update`). Hub no longer denies them as missing capability grants; `DefaultBootstrap` is unchanged.

## Known product issues affecting the harness

validate3 showed store to `hub-perm-fetch-*` ACL denials on `permission.snapshot`, so every microVM got permission snapshot v0 with 0 allowed and 0 visible. Agents then hit ACL denials on `channel.get_relevant_since.data` (Store replies with that command, not `channel.get_relevant_since`). CISO sometimes still posted from the turn payload; e2 Coder and Tester never posted within 300s. Fixed on exp/base-metrics (and arms that merge it) via the ACL changes above: `hub-perm-fetch*` prefix match, Store `channel.*` replies to role agents, and hub `permission.snapshot` pushes to `tester*`, `ciso*`, `architect*`, and `researcher*`. Still broken on main until upstreamed. See `docs/exp/base-metrics-deviations.md`. Product Go code and `permissions.json` are unchanged.

## Caveats

- **Arm A is Court-out.** Compare `court_dependent` rows separately. Do not treat Arm A egress failures as a collaboration-efficiency signal. See "Arm A (DM, Court-out)".
- **court_dependent scenarios depend on Court/egress policy behaviour; compare with care.** They are separated in `summary.md` for that reason. A pass there is a policy outcome, not a coding outcome. Egress mechanical checks require a refusal or approval stance (court, approval, unavailable, cannot fetch). Telling someone to fetch example.com is not a pass.
- **e1 / e3 may conclude on quiet without a delivered file.** See "How agents see the scratch project". Agents can discuss the task and never post a `// file:` fence; the mechanical grader then fails. That is a known limitation.
- **Serial single daemon.** One arm at a time, one daemon. Scenarios inside an arm share a warm model cache, a warm microVM pool, and whatever the host is doing. There is no parallel arm.
- **Time drift between arms.** Later arms run later. Load, model-server cache, and pool warmth differ even when `--shuffle-seed` gives every arm the same scenario order. `wall_s` across arms is not a same-hour comparison.
- **In-memory Store usage.** `llm.usage` records live in the Store process. A daemon or Store restart drops them. The after-snapshot then disagrees with the before-snapshot, and token counts for a cell that straddles the restart are wrong. The driver allows at most two health restarts per arm and keeps going; it does not repair those cells. The between-cell isolate restart (see "Run isolation") runs after that cell's snapshot, so the cell just finished is not split. It does not count toward the two-restart cap. The next cell starts from an empty store.
- **Recent API cap of 500 records.** The portal clamps `/api/llm-usage/recent` to 500 records (the newest). A run that emits more than 500 records, or a host whose other traffic pushes this run's early records out of the newest 500 before the after-snapshot, under-counts tokens. The Store process also trims its own buffer, but 500 is the cap the harness can see.
- **Second-level timestamps.** Usage `timestamp` values are UTC RFC3339 with whole seconds. Two calls in the same second are not ordered, and a record stamped on the same second as the window edge can be kept or dropped incorrectly. The harness drains after the conclusion signal, then waits a few seconds before the after-snapshot, so a call that finishes during the drain can land. That does not fix the rounding.

Token fields are comparable across arms only when both sides kept a live daemon for the whole cell, the 500-record window did not wrap, and unattributed tokens are small. Even then, do not present token deltas as experiment results until a multi-call run (PM, agent, and Court) has been checked as in "Token accounting". The pilots were PM-only because `ensure.role` ran after the plan post.

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
