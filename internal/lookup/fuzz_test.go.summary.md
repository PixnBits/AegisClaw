# fuzz_test.go

## Purpose
Provides fuzz tests for the `internal/lookup` package's pure-Go embedding and Gemma 4 formatting logic. These tests exercise the package's internal functions with arbitrary byte inputs to surface panics, crashes, or unexpected behaviours that unit tests with fixed inputs would miss. Because the embedding function and tool-block formatter must handle arbitrary tool names and descriptions from untrusted skill specs, fuzz coverage is essential for safety.

## Key Types and Functions
- `FuzzHashEmbeddingFunc`: fuzzes the FNV-32 hash embedding function with arbitrary text inputs, verifying the output vector always has 384 dimensions and is L2-normalised
- `FuzzFormatGemma4Block`: fuzzes the Gemma 4 `<|tool|>…<|/tool|>` block formatter with random tool names and JSON parameter strings
- `FuzzJsonQuote`: fuzzes the JSON string-quoting helper used when building tool block content
- `FuzzBuildIndexContent`: fuzzes the content string assembled before embedding, verifying it never panics on arbitrary name/description combinations

## Role in the System
Ensures the lookup package's foundational primitives (embedding and formatting) are robust against unexpected inputs. This is particularly important because tool metadata originates from external skill registries and governance proposals, making input sanitisation and crash-safety critical.

## Dependencies
- `testing`: Go standard fuzz testing framework
- Exercises internal functions of `internal/lookup`
