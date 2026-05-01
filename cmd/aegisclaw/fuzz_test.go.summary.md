# fuzz_test.go — cmd/aegisclaw

## Purpose
Fuzz and property-based tests for tool lookup parsing and ReAct termination logic. Runs as standard Go fuzz targets under `go test -fuzz=` and as unit tests in corpus-replay mode under `go test ./...`.

## Key Fuzz Targets
- `FuzzParseSkillToolName` — verifies `parseSkillToolName` never panics; asserts output invariants (returns `("","")` for inputs with no dot, empty parts, or reserved prefixes like `"proposal"`, `"list"`).
- `FuzzToolRegistryExecute` — feeds arbitrary tool names and JSON args into `ToolRegistry.Execute`; asserts no panic and that returned errors are non-nil for obviously invalid inputs.
- `FuzzReActTermination` — drives the ReAct loop with a scripted executor and fuzz-generated step counts; asserts that `Run` always terminates within `reactMaxIterations`.

## System Fit
Provides confidence in the robustness of the dispatch and termination paths against unexpected input. No KVM or Ollama required.

## Notable Dependencies
- Standard library only (`context`, `strings`, `testing`, `unicode/utf8`).
