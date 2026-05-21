package main

// dashboard_handlers.go previously contained makeDashboardSkillsHandler and
// makeDashboardProposalHandler. These were removed during Phase 8 cleanup
// because they were dead code (never registered) left over from the Phase 3
// TCB reduction. Dashboard / portal functionality now routes through the
// trusted portal bridge in dashboard_daemon.go.
