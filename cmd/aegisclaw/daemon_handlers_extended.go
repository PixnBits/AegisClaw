package main

// authorizeCaller and withAuthorizedCaller provide peer UID based access control.
// Phase 4.5: Socket hardening includes strict directory (0700) and file (0600) permissions
// plus this credential-based authorization.

// Note: Full peer credential validation is already present.
// This area can be further tightened in Task 04 if needed.
