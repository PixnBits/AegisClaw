# Task 03: Host Daemon TCB Boundaries

**Status**: Phase 2 (Store VM) complete. **Phase 3.1 (Audit) started**.
**Last Updated**: May 17, 2026

## Phase 3.1 Outcome: Audit of Remaining Daemon Logic

After completing Phase 2 (real Firecracker Store VM), the following control-plane logic remains in the Host Daemon:

### Fully Stubbed / Moved Out (Good)
- **Vault / Secrets**: All `vault.secret.*` handlers stubbed → Network Boundary VM.
- **Sessions**: All `sessions.*` handlers stubbed → AegisHub + Session VMs.
- **Court decisions**: Stubbed → Court Scribe / AegisHub.
- **Team & Autonomy**: Most handlers stubbed (some legacy registry access remains in a few places).
- **Tasks**: Mostly stubbed.
- **Skills (activate/deactivate/status)**: Largely stubbed or using legacy registry paths.

### Partially Remaining (Needs Attention in Phase 3)
- **Chat handlers** (`chat.message`, `chat.tool`, `chat.summarize`): Still have real implementations in `chat_handlers.go`. These should become thin proxies to AegisHub.
- **Worker list/status**: Some direct access to `env.WorkerStore`.
- **Tool Registry access**: Some direct usage in chat/skill paths.
- **EventBus coordination**: Still partially in daemon.

### Core / Expected to Stay
- Kernel control (`kernel.shutdown`, `kernel.restart`)
- Sandbox / VM lifecycle management
- Unix socket API surface + authorization
- AegisHub + Store VM launch/monitoring

## Recommended Focus for Phase 3.2+
1. Convert remaining chat handlers into thin AegisHub proxies.
2. Move Tool Registry serving fully into AegisHub.
3. Clean up any remaining direct `env.WorkerStore` / registry usage.
4. Strengthen AegisHub as the central router.

**Next**: Proceed to Phase 3.2 (Tool Registry) or 3.3 (Chat proxies).