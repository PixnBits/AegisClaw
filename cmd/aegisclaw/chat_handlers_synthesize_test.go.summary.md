# chat_handlers_synthesize_test.go — cmd/aegisclaw

## Purpose
Unit tests for `synthesizeEmptyFinalMessage` — the function that generates a human-readable fallback when the agent returns an empty final response.

## Key Tests
- `TestSynthesizeEmptyFinalMessage_VoteSuccess` — last tool `proposal.vote` with success → vote-aware message.
- `TestSynthesizeEmptyFinalMessage_VoteFailure` — last tool vote with failure → mentions failure.
- `TestSynthesizeEmptyFinalMessage_Generic` — non-vote trace → generic fallback string.
- `TestSynthesizeEmptyFinalMessage_EmptyTrace` — empty trace → safe default string.

## System Fit
Pure unit tests; no Ollama, KVM, or cassettes required. Prevents regression on empty-response UX.

## Notable Dependencies
- Standard library only (`strings`, `testing`).
