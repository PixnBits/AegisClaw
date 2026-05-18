package main

// Approvals handlers now proxied to AegisHub (Phase 3.4)
apiSrv.Handle("approvals.list", makeApprovalsListProxy(env))
apiSrv.Handle("approvals.decide", withAuthorizedCaller(env, "approvals.decide", makeApprovalsDecideProxy(env)))
