# Performance Targets & Virtualization Strategy

**Status**: Target State

## Overview

This document defines performance targets and strategies for handling large amounts of data in the Web Portal (long activity feeds, traces, Canvas views with many agents, etc.). Clear targets help implementors make good architectural decisions early.

## Key Performance Areas

### Concurrent Connections & Real-time

- Target: Support at least 50–100 concurrent WebSocket / STOMP connections per portal instance under normal load.
- Heartbeat and idle connection cleanup must be aggressive to prevent resource exhaustion.
- Real-time updates should feel responsive (< 100–200ms perceived latency for most events under normal conditions).

### Activity Feeds & Timelines

- Target: Smooth rendering and scrolling for feeds containing 500–2000+ events without significant lag.
- Virtualization (virtual scrolling) should be used once the visible list exceeds ~100–150 items.
- Search and filtering within feeds should remain responsive even on large datasets.

### Single-Agent Traces

- Target: Comfortable handling of traces with 200–1000+ phases/tool calls.
- Virtualization or progressive loading / collapsing should be applied for very long traces.
- Expanding a tool call or phase should feel instant.

### Canvas / Inter-Agent Views

- Target: Smooth rendering of 20–50+ concurrent agents/tasks.
- Updates to individual agent status or progress should not cause full re-renders of the view.
- Visual pipeline indicators should update efficiently.

### Dashboard / Monitoring

- Target: Quick loading and filtering of lists containing hundreds of active agents/tasks.
- Metrics cards should update in near real-time without expensive re-computation on the client.

## Virtualization Strategy

- Use virtual scrolling libraries or custom implementations for long lists (activity feeds, traces, member lists under high cardinality).
- Prefer "windowing" techniques that only render items currently in or near the viewport.
- For traces and feeds, consider hybrid approaches: keep recent items fully rendered + virtualized older items.
- Maintain good keyboard navigation and accessibility even when virtualization is active.

## Data Handling Recommendations

- Prefer incremental updates (deltas) over full list replacements when possible.
- Implement client-side caching with time-based or size-based eviction where appropriate.
- For very large histories, consider server-side pagination or cursor-based loading with "Load more" or infinite scroll patterns.

## Resource Budgets (Portal VM)

- The portal VM should remain lightweight. Real-time processing and rendering should not cause excessive memory or CPU usage.
- Target: Keep portal memory usage under control even with dozens of concurrent users and long-lived connections.
- Monitor and set alerts for connection count, memory growth, and event processing rate on the Host side.

## Implementation Notes

- Choose virtualization solutions that are lightweight and have good accessibility support.
- Test performance early with realistic data volumes (synthetic long feeds and traces).
- Profile rendering performance in the browser during development of feed and trace views.

Clear performance targets and a defined virtualization strategy will prevent major refactoring later when real usage data appears.