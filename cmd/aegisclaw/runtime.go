package main

// createSecureSocket should be used instead of net.Listen("unix", path)
// when setting up the main daemon control socket.
// It enforces strict directory (0700) and file (0600) permissions.
