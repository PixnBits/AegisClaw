# store.go

## Purpose
Implements a persistent JSON store for worker agent records. Workers are ephemeral sub-agents spawned by the main orchestrator to perform specialised subtasks (research, coding, summarisation). The store tracks each worker's full lifecycle from spawning through completion, recording the role, assigned tools, task description, status, result, and error. The backing file is a JSON array sorted by `SpawnedAt` and written atomically via `.tmp` + rename.

## Key Types and Functions
- `Role`: typed string — `RoleResearcher`, `RoleCoder`, `RoleSummarizer`, `RoleCustom`
- `WorkerStatus`: typed string — `StatusSpawning`, `StatusRunning`, `StatusDone`, `StatusFailed`, `StatusTimedOut`, `StatusDestroyed`
- `WorkerRecord`: WorkerID (UUID), VMID, Role, TaskDescription, ToolsGranted ([]string), TaskID, SpawnedBy, SpawnedAt, FinishedAt (*time.Time), TimeoutAt, Status, Result, Error, StepCount
- `Store`: wraps a JSON file path and a `sync.RWMutex`
- `NewStore(dir string) (*Store, error)`: creates or opens the store; creates the directory if absent
- `Upsert(record WorkerRecord) error`: inserts or updates by WorkerID; re-sorts by SpawnedAt; atomic write
- `Get(id string) (*WorkerRecord, error)`: lookup by WorkerID; returns a deep copy
- `List(activeOnly bool) ([]WorkerRecord, error)`: returns all records or only those in active states
- `CountActive() (int, error)`: count of workers in spawning/running state
- `cloneWorkerRecord`: deep copy including ToolsGranted slice and FinishedAt pointer

## Role in the System
Used by the daemon orchestrator to track and manage the lifecycle of sub-agent worker VMs. The store enables the orchestrator to resume monitoring after a restart and provides the data for the status dashboard's worker view.

## Dependencies
- `encoding/json`: JSON array persistence
- `os`, `sync`: atomic file writes and concurrency
- `github.com/google/uuid`: WorkerID generation
