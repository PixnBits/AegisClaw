package main

// Timers and Signals now proxied (Phase 3.4)
apiSrv.Handle("timers.list", makeTimersListProxy(env))
apiSrv.Handle("signals.list", makeSignalsListProxy(env))
