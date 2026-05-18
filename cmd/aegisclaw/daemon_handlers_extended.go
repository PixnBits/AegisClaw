package main

// Skills and Tasks section - Phase 3.4 Cleanup
// Most skill and task management has moved to AegisHub + dedicated VMs.
// Remaining handlers are thin stubs or proxies.

// Legacy direct registry access in some skill paths has been deprecated.
// All new skill lifecycle operations should go through AegisHub.

const skillTaskMovedMsg = "skill/task operations have moved to AegisHub + Skill VMs (see proxy layer)"

// Example cleaned stub (already in place from earlier phases)
// apiSrv.Handle("skill.list", func(...) { return error with skillTaskMovedMsg })

// Tasks are similarly stubbed.
// No new direct env.Registry or env.WorkerStore access should be added here.
