# Agent Runtime VM Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Agent Runtime VM is the execution environment for a single agent. It is **stateless** — all conversation state lives in the paired Memory VM.

## Core Product Requirement: Conversation Model

The agent must support **true bidirectional, asynchronous communication** as defined in the PRD:

- The user and agent can send messages at any time (no strict turn-taking)
- The agent may proactively initiate contact with updates, discoveries, or questions
- The agent may run long-lived background tasks and report back when complete or when relevant events occur
- The system must gracefully handle interleaved streams of activity without losing context
- The interface must clearly distinguish between user-initiated and agent-initiated messages

## Agent Loop (6-Step)

The Agent Runtime follows this structured loop on each iteration:

1. **Observe** — Request current context from the Memory VM (includes all recent interleaved messages)
2. **Think** — LLM performs reasoning about the current state and goals
3. **Plan** — Creates a short, explicit plan for this turn (including whether to respond now or continue working)
4. **Act** — Decides on one action: tool call, final response to user, or continue background work
5. **Execute** — Sends action to AegisHub
6. **Judge** — Evaluates result and decides next step

The loop continues until the agent chooses to send a message to the user.

## Key Behaviors

- The agent must be able to run long-lived tasks in the background
- The agent must be interruptible — new user messages must be processed promptly
- The agent may proactively send messages without being prompted
- All state (including interleaved messages) is managed exclusively by the Memory VM

## Architecture

- Runs as a dedicated Firecracker microVM
- Completely stateless
- All memory and conversation history is retrieved from the paired Memory VM on each turn

## Communication Rules

The Agent Runtime VM may only communicate with:
- Its paired **Memory VM**
- **AegisHub** (for all tool/skill calls)

It must never talk directly to the Store VM, Network Boundary VM, or other agents.

## Test Requirements

- Agent must correctly handle interleaved user and agent messages
- Agent must be able to proactively initiate messages
- Long-running background tasks must not block processing of new user messages
- Crash + restart of Agent Runtime VM must not lose conversation state
- The agent must correctly request full interleaved context from Memory VM on every turn
