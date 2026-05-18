package main

// In registerExtendedDaemonAPI, chat handlers are now proxied via AegisHubClient (Phase 3.3).

// Example updated registration (conceptual):
// apiSrv.Handle("chat.message", makeChatMessageProxy(env))
// apiSrv.Handle("chat.tool", makeChatToolProxy(env))

// The actual makeChat*Proxy functions would call env.AegisHubClient.ForwardChatMessage(...)

// For now we keep the existing makeChat*Handler calls but mark them for migration.
// Real proxy implementation will replace direct logic in chat_handlers.go.
