# store.go

## Purpose
Implements a git-backed proposal store where each proposal lives on its own dedicated git branch (`proposal-<uuid>`). The main branch holds a JSON index of all proposal metadata for fast listing. This design provides a natural audit trail — every state transition is a git commit — and isolates proposals from one another, enabling concurrent access without locking conflicts.

## Key Types and Functions
- `Store`: wraps a `go-git` in-memory or on-disk repository
- `NewStore(repoPath string) (*Store, error)`: opens or initialises the git repo; creates the main branch index if absent
- `Create(ctx, *Proposal) error`: creates a new branch for the proposal and commits its JSON; updates main index
- `Update(ctx, *Proposal) error`: commits updated proposal JSON to its branch; updates main index
- `Get(ctx, id string) (*Proposal, error)`: reads proposal JSON from its branch HEAD
- `List(ctx) ([]*Proposal, error)`: reads all proposals from the main branch index
- `ListByStatus(ctx, status) ([]*Proposal, error)`: filters the main index by proposal status
- `Import(ctx, *Proposal) error`: imports an externally-created proposal, preserving its ID
- `ResolveID(ctx, prefix string) (string, error)`: expands a UUID prefix to a full proposal ID

## Role in the System
The proposal store is the persistence layer for the Governance Court. The court dashboard, CLI commands, and the main orchestrator all read and write proposals through this store. The git-backed format provides a tamper-evident history compatible with standard git tooling.

## Dependencies
- `github.com/go-git/go-git/v5`: git operations
- `encoding/json`: proposal serialisation
- `crypto/sha256`: for index checksums
