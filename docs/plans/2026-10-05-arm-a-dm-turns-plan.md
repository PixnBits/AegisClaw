# Arm A DM transcripts and the turn definition

Harness-only. Arm A product code stays on its own branch. Judge prompt bytes stay as they are.

## Decisions

- A turn is one LLM-bearing agent or PM response that produces an outbound user-visible message: a channel post or a DM `chat.message` counted in the transcript. One message, one turn, for every arm. User and Court messages are not turns.
- `turns` is that count and `turns_source` is `transcript` when a transcript was collected. `trace` (`channel.turn.recv`) and `llm_calls` remain only when no transcript was collected. Do not compare those fallbacks with transcript turns. Older pilot rows used the trace count.
- Arm A (`../dm-no-channels`, alias `dm`) sets `"messaging": "dm"`. Arm B stays `../asd-ste100` (alias `ste`). `base,dm,ste` resolves to `base,A,B`.
- Each poll still uses `channel get`. For `messaging: dm`, also merge DM messages from a build-dir artifact the arm documents (`scripts/profile/dm-dump/<channel>.json`, `dm_files`, or `scripts/profile/harness_dm.json`) and, when no CLI is documented, from `dm dump` / `chat dump`. A missing command or a bad file does not fail the run. The same role, sender, and content are not counted twice.
- Undated DM messages are inserted so a later channel PM synthesis stays after the DM plan and the agent reply. `final_marker` is otherwise unchanged: a marker counts only on a PM or agent message at or after the first agent reply that follows the first PM message.
- Arm A is Court-out. Compare `court_dependent` rows separately. An Arm A egress failure is not a collaboration-efficiency signal.
- A scenario may set `"messaging": "dm"` or `"channel"` and override the arm.

## Out of scope

- Product DM routing, a new `aegis` subcommand, rootfs builds, and the judge prompt text.
- Changing how tokens are attributed.

## Tests

- Mirrored channel posts with an agent sender, and a merged DM plan plus agent plus channel synthesis, both fire `final_marker`.
- Explicit `role` `pm` or `agent` wins over sender classification. Judge prompt text is unchanged.
- Aliases, the DM file reader, unknown-command caching, and path escape.
- Existing profile unittests.
