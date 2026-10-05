# Ensure.role before plan post, system sender, Ollama cache-adjusted tokens

Harness + PM scheduling fix. Do not commit or push from this work.

## Decisions

- In `pmProcessPlanningMessage`, compute `rolesToEnsure` from the plan text and send `ensure.role` (and the existing CISO `channel.add_member` block) **before** `channel.post`. `hcl.Send` for `ensure.role` is request/response; the daemon adds the member before replying. Fallback, claim/release, logging, and post-failure release stay as they are. No keyword lists, few-shots, or scenario fixtures.
- `classify_sender("system")` returns `"system"`. That role is not user, pm, court, or agent: it does not count as a turn, an agent message, or the agent reply that makes `final_marker` eligible.
- API `prompt_eval_count` (copied into `llm.usage.record` `tokens_prompt`) is the **full** prompt on Ollama 0.33.2. Journal `prompt eval time ... / N tokens` N is **newly evaluated** (cache-adjusted). `tokens_prompt` / `tokens_prompt_raw` stay the attributed API sum. After a live run, journalctl for the run window sets `tokens_prompt_cache_adjusted`, `journal_llm_calls`, and `tokens_cache_method` `api_prompt_eval_count_full__journal_prompt_eval_new`. Journal failure or empty timing lines leave cache-adjusted null and method `api_prompt_eval_count_full__journal_unavailable`. Journal failure must not fail the run. `--dry-run` does not call journalctl.
- `check_ollama_journal.py` compares journal **completion** sum and **prompt-eval line count** to the result. It reports journal prompt sum as cache-adjusted and does not fail when that sum is below API raw.
- `llm_calls` stays the attributed usage-record count. Unattributed (e.g. Court) records stay separate.
- `summarize.py` tolerates null cache-adjusted values and adds a median column. README gets a Token accounting section (raw vs cache-adjusted, how to re-verify, attribution, Court unattributed, pilot1/pilot2 single-call root cause and fix).

## Out of scope

- Committing, pushing, rootfs rebuilds, changing `extractRolesFromText`, disabling the Ollama cache, changing usage attribution.

## Tests

- Go: fake hub records command order; `ensure.role` (and CISO `channel.add_member` when it fires) precede the plan `channel.post`. Existing PM claim/release/post-error tests still pass.
- Python: system status after the PM plan does not set `marker_hit` and does not count as an agent message or a turn.
- Journal helper: cache-hit prompt sum below raw is not exit 1; completion or call-count mismatch still is.
- `run_one` journal overlay: cache-hit sets adjusted tokens; unavailable/empty sets null. `build_result` allows adjusted != raw. Summarize median skips nulls.
- `go build ./... && go vet ./cmd/project-manager/... && go test -count=1 ./cmd/project-manager/...` and `python3 -m unittest discover -s scripts/profile -p 'test_*.py'`.
