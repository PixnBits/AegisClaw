# ADR-001: Memory VM Architecture for the Multi-Agent Collaboration Model

**Status:** Proposed
**Date:** 2026-06-06
**Deciders:** Nick (PixnBits)

## Context

The new collaboration model (see `docs/prd/collaboration-model.md`) introduces significantly more dynamic agents than before: Court personas as first-class Agents, SDLC role agents, a central Project Manager Agent, and on-demand general agents. All of these will be spun up and down frequently.

Currently, each agent has a dedicated pair of microVMs:
- `agent-<sid>` (the runtime / ReAct loop)
- `memory-<sid>` (short-term context + long-term memory, acting as the single source of truth for that agent's state)

This design provides strong isolation: memory access is enforced at the Firecracker VM boundary, making cross-agent memory leakage structurally difficult.

However, with many more short-lived agents, maintaining two microVMs per agent creates pressure on:
- Resource usage (especially on laptop-class hardware)
- Startup latency (conflicts with the `<1s` agent availability target)
- Operational complexity (managing many transient VMs)

We are therefore evaluating whether to change the memory storage architecture.

## Decision Drivers

- **Security is paramount** (paranoid isolation model, least privilege, compartmentalization, blast radius containment).
- The collaboration model requires fast, on-demand agent availability.
- We want to preserve (or improve) the guarantee that one agent cannot read or tamper with another agent's memory.

## Options Considered

### Option 1: Keep per-agent Memory VMs (status quo)
Each agent continues to get its own dedicated `memory-<sid>` microVM.

**Pros:**
- Strongest isolation (hardware boundary + narrow IPC).
- Clear blast radius containment.
- Easy to reason about and audit cross-agent isolation.
- Aligns with existing "every major function has its own security boundary" principle.

**Cons:**
- Highest VM count (2 per agent).
- Higher resource usage and slower aggregate startup when many agents are active.

### Option 2: Single Shared Memory VM for all agents
One central Memory VM serves memory for every agent in the system.

**Pros:**
- Lowest VM count.
- Simplest lifecycle management.
- Easier to implement advanced memory features centrally.

**Cons:**
- Cross-agent isolation becomes a software problem inside one high-value component.
- Much larger blast radius if the shared Memory VM is compromised.
- Concentrates trust in a single multi-tenant service.
- Significantly weakens the current compartmentalization guarantees.
- Increases complexity and attack surface of the Memory VM itself (authentication, authorization, namespacing, quota enforcement).

### Option 3: Small number of sharded Memory VMs (e.g. 4–8)
A fixed small pool of Memory VMs, with agents assigned to shards (e.g. by hash of agent ID).

**Pros:**
- Significantly reduces total VM count vs per-agent.
- Limits blast radius compared to a single shared VM.
- Still allows hardware-enforced isolation between shards.
- More practical for pre-warming and resource management.

**Cons:**
- Still requires robust internal access control within each shard.
- Adds some sharding logic and operational complexity.
- Not as strong as full per-agent isolation.

### Option 4: Merge memory logic into the Agent VM
Move memory handling inside the `agent-<sid>` microVM (e.g. as a restricted subprocess or library).

**Pros:**
- Simplest from a VM count perspective.

**Cons:**
- Directly violates the compartmentalization principle.
- Dramatically increases blast radius (agent compromise = memory compromise).
- Loses the current strong guarantee that memory is protected behind its own security boundary.
- **Rejected** on security grounds.

## Decision

**We will pursue Option 3 (small number of sharded Memory VMs) as the primary direction**, combined with aggressive pre-warming and snapshot optimizations for the remaining VMs.

We explicitly **reject Option 2 (single shared Memory VM)** and **Option 4 (merge into agent VM)** because they too strongly conflict with AegisClaw’s paranoid security model and the goal of strong compartmentalization between agents.

We will keep Option 1 (per-agent) as a fallback for high-trust or long-lived agents if needed.

## Consequences

### Positive
- Better balance between security and the performance/resource requirements of the new collaboration model.
- Maintains hardware-enforced isolation between groups of agents.
- Reduces total concurrent microVMs significantly compared to the current per-agent pairs.
- Aligns with the pre-warm readiness work already happening on this branch.

### Negative / Risks
- Introduces some additional complexity in agent-to-memory routing and shard assignment.
- Requires careful design of the Memory VM’s internal access control and namespacing.
- The decision increases the importance of the pre-warming and fast-startup work on this branch.

### Neutral
- The Project Manager Agent and Court personas will still get their own memory isolation (either dedicated or sharded).

## Alternatives
- Stick strictly with per-agent memory VMs and optimize startup time instead (still viable).
- Revisit this decision after the first version of the collaboration model is running and we have real usage data on concurrent agent counts.

## Open Questions
- What is the optimal number of shards?
- How will the Project Manager and Court agents be assigned (dedicated shards vs shared)?
- What authentication/authorization mechanism will the Memory VMs use (vsock + capability tokens, mTLS, etc.)?
- Should memory snapshots be per-shard or per-agent?