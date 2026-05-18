package main

// In registerExtendedDaemonAPI, switch to proxies (Phase 3.3):

// OLD (kept for reference during transition):
// apiSrv.Handle("chat.message", makeChatMessageHandler(env, toolRegistry))
// apiSrv.Handle("chat.tool", withAuthorizedCaller(env, "chat.tool", makeChatToolExecHandler(env, toolRegistry)))

// NEW - Phase 3.3 proxies:
apiSrv.Handle("chat.message", makeChatMessageProxy(env))
apiSrv.Handle("chat.tool", withAuthorizedCaller(env, "chat.tool", makeChatToolProxy(env)))

// Note: makeChatSlashHandler and makeChatSummarizeHandler can follow the same pattern.
