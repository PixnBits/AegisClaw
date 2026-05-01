# system_handlers.go — cmd/aegisclaw

## Purpose
Implements the `system.stats` daemon API handler, returning real-time host resource metrics sampled from `/proc`.

## Key Types / Functions
- `systemStatsResponse` — `{ host_ram_total_mb, host_ram_used_mb, host_ram_pct, host_load_avg_1, host_load_avg_5, host_load_avg_15 }`.
- `makeSystemStatsHandler()` — returns an `api.Handler` that calls `readSystemStats()`.
- `readSystemStats()` — reads `/proc/meminfo` for RAM totals and available, and `/proc/loadavg` for the three load averages.

## System Fit
Powers the resource-usage display in the web dashboard. No external dependencies — reads directly from the Linux proc filesystem.

## Notable Dependencies
- Standard library only (`bufio`, `os`, `strconv`, `strings`).
