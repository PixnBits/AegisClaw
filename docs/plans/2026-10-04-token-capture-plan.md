# Token capture and Ollama caching

Supersedes the token bullet in `docs/plans/2026-10-04-profile-env-conclusion-plan.md`.

Checked offline against pilot `pilot-20261004-230206` and `journalctl -u ollama`. bday and e2 usage rows matched the PM `/api/generate` journal lines (`prompt_eval_count` / `eval_count`, e2 671/2581). Those runs had no agent or Court LLM turns. KV-cache similarity (for example 254/671) did not reduce `prompt_eval_count`.

## Decisions

- `tokens_prompt` and `tokens_prompt_raw` stay the attributed sum of `prompt_eval_count` (`llm.usage.record` field `tokens_prompt`).
- `tokens_prompt_cache_adjusted` equals that sum. `tokens_cache_method` is `prompt_eval_count_equals_full_prompt_on_host`.
- Do not subtract a cache-similarity fraction. If a later call shows `prompt_eval_count` below the full prompt, prefer disabling the Ollama cache via options, or read journal task `n_tokens`. That case has not been observed.
- `tokens_unattributed` stays prompt plus completion on unattributed delta records. Also record `tokens_prompt_unattributed` and `tokens_completion_unattributed`.
- Token deltas are not experiment results until the same journal check is done on a run that actually called the PM, an agent, and Court.
- Judge prompt bytes stay unchanged. `score.py` already records `judge_prompt_sha256` of those bytes.

## Out of scope

- Disabling the Ollama cache, changing attribution, or changing the judge prompt.
- Publishing token comparisons from `summary.md`.

## Tests

- Account and `result.json` equality, method string, and the unattributed split.
- Journal helper sums prompt-eval and eval timing lines in the run window and ignores cache similarity.
- Existing profile unittests, including the judge prompt hash.
