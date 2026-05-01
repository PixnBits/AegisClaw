# Package: runtime

## Overview
The `runtime` package tree contains the agent task execution subsystem. Currently it has one sub-package:

- **`runtime/exec`** — the `TaskExecutor` interface, ReAct FSM, and all executor implementations (Firecracker production, in-process test, and Ollama cassette recorder)

See [`internal/runtime/exec/summary.md`](exec/summary.md) for full documentation of that sub-package.

## System Role
The runtime layer sits between the orchestration/TUI layer and the sandbox layer. It takes a task description, drives the ReAct reasoning loop, and dispatches tool calls into the appropriate execution environment (Firecracker VM in production, in-process Ollama in tests).
