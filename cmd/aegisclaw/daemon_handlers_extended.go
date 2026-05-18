package main

// Sessions handlers now fully proxied to AegisHub (Phase 3.4)
apiSrv.Handle("sessions.list", makeSessionsListProxy(env))
apiSrv.Handle("sessions.history", makeSessionsHistoryProxy(env))
apiSrv.Handle("sessions.send", makeSessionsSendProxy(env))
apiSrv.Handle("sessions.spawn", makeSessionsSpawnProxy(env))
