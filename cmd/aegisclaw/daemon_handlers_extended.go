package main

// Worker handlers now proxied to AegisHub (Phase 3.4)
apiSrv.Handle("worker.list", makeWorkerListProxy(env))
apiSrv.Handle("worker.status", makeWorkerStatusProxy(env))
