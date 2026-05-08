# Court Scribe VM Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Court Scribe VM acts as the **Clerk of the Court** — a lightweight notification and coordination service. It does **not** distribute the actual proposal or code content.

## Responsibilities

- Notify the five Governance Court VMs when there is new material to review
- Track which Court personas have completed their review
- Collect votes and structured feedback from the Court VMs
- Notify the original Agent or Builder VM when the review process is complete
- Maintain the overall state of the review process (e.g. "In Review", "All Votes Received")

## Core Principle

The Court Scribe **never** sees or transmits the actual proposal text or code changes. Court VMs retrieve content directly from the Store VM.

## Architecture

- Very lightweight dedicated Firecracker microVM
- Stateless design
- Only responsible for coordination and state tracking

## Communication Flow

1. Store VM tells Court Scribe that a new proposal or code review is ready
2. Court Scribe notifies all five Court VMs with the proposal ID
3. Each Court VM independently pulls the proposal directly from the Store VM
4. Court VMs send their vote and feedback back to the Court Scribe
5. Court Scribe aggregates votes and notifies the proposer when complete

## Key Commands

- `scribe.notify_review` — Notify Court VMs that a new review is ready
- `scribe.submit_vote` — Court VM submits its vote and feedback
- `scribe.get_review_status` — Get current status of a review

## Security Requirements

- The Scribe must never receive or transmit proposal content
- All votes must be cryptographically signed by the sending Court VM
- A compromised Scribe must not be able to forge votes or suppress feedback
- The Scribe must enforce that all five Court personas have voted before advancing a proposal

## Test Requirements

- Court Scribe must never be sent proposal content
- All five Court VMs must independently pull content from the Store VM
- Scribe must not be able to approve a proposal without all five votes
- Malicious Scribe must not be able to drop or modify votes
- Review state must be accurately tracked
