# workspace_test.go

## Purpose
Tests for the workspace `Load` function covering all edge cases: empty directory, missing directory, all four files present, partial files (only some files present), oversized file rejection, and a path that is not a directory. Tests verify that missing files are silently ignored and that `IsEmpty` correctly detects absence of all workspace content.

## Key Types and Functions
- `TestLoad_EmptyDir`: creates an empty temp directory; verifies `Load` returns a non-nil `Content` with all empty fields and `IsEmpty()` returns true
- `TestLoad_MissingDir`: calls `Load` with a non-existent path; verifies no error is returned (absent workspace is not an error)
- `TestLoad_EmptyPath`: calls `Load` with an empty string path; verifies graceful handling
- `TestLoad_AllFiles`: writes all four workspace files; verifies each field in `Content` contains the file's trimmed content
- `TestLoad_PartialFiles_OnlySOUL`: writes only SOUL.md; verifies `Soul` is populated and the other three fields are empty
- `TestLoad_FileTooLarge`: writes a file larger than 16 KiB; verifies `Load` returns a size error
- `TestLoad_NotDirectory`: creates a regular file at the workspace path; verifies `Load` returns an error indicating the path is not a directory
- `TestIsEmpty`: verifies `IsEmpty` returns true for zero-value `Content` and false when any field is set

## Role in the System
Ensures workspace loading is robust against missing, partial, and malformed installations. Since the workspace is user-provided input that is injected into the agent's system prompt, the size cap enforcement is security-critical.

## Dependencies
- `testing`, `t.TempDir()`, `os`
- `internal/workspace`: package under test
