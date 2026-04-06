---
enabled: true
interval: 30s
consolidation_enabled: true
consolidation_interval: 5m
consolidation_idle_threshold: 1m
max_consolidation_duration: 10m
memory_tools:
  - Add_memory
  - Update_memory
  - Retrieve_memory
---

# Heartbeat Configuration

The heartbeat subsystem emits periodic events to keep the agent engaged and maintain context awareness.

## Settings

- **enabled**: Enable/disable the heartbeat subsystem.
- **interval**: How frequently heartbeat events are emitted (e.g., `30s`, `1m`).
- **consolidation_enabled**: Enable periodic memory consolidation.
- **consolidation_interval**: How often consolidation is triggered (e.g., `5m`).
- **consolidation_idle_threshold**: Minimum idle time before consolidation can occur (e.g., `1m`).
- **max_consolidation_duration**: Maximum wall-clock time consolidation can run (e.g., `10m`).
- **memory_tools**: List of memory tool names to invoke during consolidation (e.g., `Add_memory`, `Update_memory`, `Retrieve_memory`).

## Notes

Consolidation uses the listed memory tools to summarize and improve memory state. Adjust intervals and thresholds to balance memory freshness with system load.
