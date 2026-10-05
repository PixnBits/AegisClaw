# exp/base-metrics deviations

This branch deliberately diverges from main so the efficiency experiment can run. The changes below are exp-only. They are not proposed for main in this experiment. Arms that merge this branch pick them up. Main stays as it is until someone upstreams them on purpose.

## PM ensure.role before plan channel.post

Already on this branch. `cmd/project-manager` sends `ensure.role` (and the CISO `channel.add_member` block, when that block runs) before the plan `channel.post`.

On main, `ensure.role` ran after the plan post. The facilitator scheduled turns for the plan while the only channel member was the project manager, skipped that post as a self-post, and Coder or CISO joined about a second later with no further `channel.updated` to schedule a turn. Result: one LLM call, the PM plan, and no agent turn.

`ensure.role` is a request/response. The daemon adds the member before the send returns, so those roles are members when the plan is posted.

## ACL: hub-perm-fetch* and Store channel replies

`config/acls.yaml` on this branch differs from main. No product Go change and no `permissions.json` change go with this edit.

Permission snapshot RPC wait ids are `hub-perm-fetch-<nanos>` (`cmd/aegishub/permissions.go`). The exact id `hub-perm-fetch` never matched those endpoints, so every `store -> hub-perm-fetch-* : permission.snapshot` was denied. Every microVM got snapshot v0 (0 allowed, 0 visible). Both directions now use the prefix `hub-perm-fetch*`, which also covers the exact id:

- `hub-perm-fetch*` → store: `permission.snapshot`, `permission.request`
- store → `hub-perm-fetch*`: `permission.snapshot`, `error`, `response`

Agents request `channel.get_relevant_since`. Store replies with command `channel.get_relevant_since.data`. Court personas already had `store → court-persona-* : channel.*`. Role agents did not, so live denials were `store -> coder-… : channel.get_relevant_since.data` and `store -> ciso-… : channel.get_relevant_since.data`. Store may now reply with `channel.*` to `agent*`, `coder*`, `tester*`, `ciso*`, `architect*`, `researcher*`, and `project-manager*`. The destination is not `"*"`.

Hub already pushed `permission.snapshot` to `agent*`, `project-manager*`, and `coder*`. The same push now also targets `tester*`, `ciso*`, `architect*`, and `researcher*`.

## ACL: daemon-internal* llm.usage.*

`daemon-internal*` and `daemon-internal-*` may send `llm.*` and `llm.usage.*` to store. No product Go change. Portal usage API is host-bridged as daemon-internal.

## Hub: deliverPendingRPC accepts permission.snapshot RPC replies

Store replies to `hub-perm-fetch-*` with command `permission.snapshot`, the same name as the unsolicited Hub→agent push, so `deliverPendingRPC` delivers that reply only when the waiter requested `permission.snapshot`.

## Permissions: turn_result/add_member ACL-only

`IsCapabilityCommand` excludes `channel.turn_result`, `channel.add_member`, `channel.turn`, and `channel.member_turn_update` as ACL-gated collaboration plumbing; they are not capability grants and are not added to `DefaultBootstrap`.

## Hub: older close does not drop a re-registration

Already on this branch. `cmd/aegishub` replaces `registered[id]` when the same component registers again. The previous connection's close used to delete that map entry and the `conns` slot by id alone. If that close ran after the replacement, it removed the new registration. Later frames on the new connection were `ERR_UNAUTHORIZED` (`Audit: unauthorized connection <id>`). Portal reads that use the new connection then failed, so usage APIs returned empty while LLM calls had still happened.

The close path now deletes `registered[id]` only when that entry's encoders are still this connection's, and removes the `conns` entry only when it is still this connection. The connection that still owns the id still clears it when it closes. No ACL change. No guest image change.
