# `reviewer_test.go` — Tests for Reviewer Wire Protocol

## Purpose
Unit-tests the `ReviewResponse` parsing and validation logic — specifically the `UnmarshalJSON` flexibility and `Validate()` schema gate — without requiring a live sandbox.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestReviewResponse_UnmarshalJSON_ArrayEvidence` | Parses `evidence` as a JSON array correctly |
| `TestReviewResponse_UnmarshalJSON_StringEvidence` | Handles LLMs returning `evidence` as a plain string |
| `TestReviewResponse_UnmarshalJSON_EmptyEvidence` | Empty/missing evidence field does not error during unmarshal |
| `TestReviewResponse_Validate_Valid` | A fully populated response with valid fields passes validation |
| `TestReviewResponse_Validate_InvalidVerdict` | Unknown verdict string returns validation error |
| `TestReviewResponse_Validate_BadRiskScore` | Risk score outside [0, 10] returns error |
| `TestReviewResponse_Validate_MissingComments` | Empty comments field returns error |
| `TestReviewResponse_Validate_NoEvidenceNonAbstain` | Missing evidence on non-abstain verdict returns error |
| `TestReviewResponse_Validate_AbstainNoEvidence` | Abstain verdict is valid without evidence |

## Role in the System
Guards the D7 compliance requirement (98%+ structured JSON success rate). Ensures the parser is resilient to LLM quirks before responses reach the consensus evaluator.

## Notable Dependencies
- Package under test: `court`
- Standard library only
