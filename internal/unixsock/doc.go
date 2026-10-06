// Package unixsock creates mode-0600 UNIX listeners and reads a peer uid
// without calling (*net.UnixConn).File.
//
// File dups the descriptor and clears O_NONBLOCK on the shared open file
// description. Deadlines then stop working, and Close cannot interrupt a
// blocked Read. PeerUID uses SyscallConn.Control instead.
package unixsock
