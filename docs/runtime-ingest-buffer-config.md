# Runtime ingest buffer commands

`arcli` provides commands to inspect and change Arc's runtime ingest buffer limits through the administrator API. By default, Arc applies and persists a change so the values return after restart. Use `--persistent false` for a process-only change.

## Commands

```sh
# Show the effective values
arcli ingest buffer show

# Change either limit or both
arcli ingest buffer set --max-buffer-size 200000
arcli ingest buffer set --max-buffer-age-ms 30000
arcli ingest buffer set --max-buffer-size 200000 --max-buffer-age-ms 30000 --persistent true

# Persist the currently active values without changing thresholds
arcli ingest buffer set --persistent true

# Apply a value until Arc restarts
arcli ingest buffer set --max-buffer-age-ms 15000 --persistent false

# Remove the saved override and restore Arc's startup values
arcli ingest buffer reset
```

`set` requires a threshold or `--persistent true`. Persistence defaults to true for compatibility. An omitted threshold keeps its current effective value. Supplied values must be greater than zero; Arc also rejects buffer ages too large to represent as a duration. `reset` calls Arc's `DELETE /api/v1/config/runtime/ingest` endpoint; it does not restart Arc.

## Configuration scope and buffers

The thresholds configured by `set` are shared by one Arc process and apply independently to each logical ingest buffer, keyed by database and measurement. `max_buffer_size` is not a process-wide record cap, so multiple active measurements can collectively hold more records than the configured value. Arc's buffer shards partition those per-measurement buffers to reduce lock contention. Flush workers process queued flush tasks; they do not create or own separate ingest buffers or threshold settings.

## Connection and permissions

The commands use the same connection selection as other `arcli` commands: a saved connection profile or the `ARC_ENDPOINT` and `ARC_TOKEN` environment variables. When Arc authentication is enabled, the token must have administrator privileges.

For the local Compose lab, run:

```sh
docker compose exec arcli arcli ingest buffer show
docker compose exec arcli arcli ingest buffer set --max-buffer-size 200000 --max-buffer-age-ms 30000
docker compose exec arcli arcli ingest buffer reset
```

In a cluster, `arcli` requires every node to report healthy, reads each node's current settings, then applies the change to every node. This is coordinated fan-out, not Raft replication or a distributed transaction. If a request fails partway through, it attempts to restore settings on nodes that may have changed, including the node whose request returned an error because the server may have committed before the connection failed. Rollback is best-effort; rollback failures are reported and can leave a partial update that an operator must reconcile. Each Arc node persists the override in its own metadata SQLite database. Direct API calls remain process-local and do not contact peers.

## Ingest write-path cost

Arc checks the configured size threshold once per buffered Arrow batch, not once per record. That check existed before runtime reconfiguration; the runtime API replaces the immutable startup-field read with one atomic in-memory load per batch so a change can take effect without rebuilding the writer. Ingest writes do not read environment variables, `arc.toml`, SQLite, or the API. The persistent SQLite read happens at startup, and a persistent write happens only when an administrator changes the setting.

The age threshold is consumed by Arc's background flusher, not polled by each ingest write. Changing it signals the flusher to recalculate its deadline. The atomic load replaces an ordinary field read; its workload-specific cost has not been quantified by a benchmark.

## Output

The default table output includes the effective `max_buffer_size`, `max_buffer_age_ms`, scope, persistence state, and source. Use `--output json` (or `-o json`) for scripts:

```sh
arcli ingest buffer show --output json
arcli ingest buffer set --max-buffer-age-ms 30000 --output json
arcli ingest buffer reset --output json
```

Arc reports `source: "persistent_override"` while an override exists, `source: "runtime_override"` for process-only changes, and `source: "startup_config"` after reset. `persistent` reports whether Arc has a saved override.

## Grafana update delay

`arcli ingest buffer show` reads Arc's current process values directly. It does not trigger the collector's next telemetry sample, flush Arc's buffer, or refresh Grafana. A Grafana panel backed by stored telemetry can therefore lag behind `show` after a setting changes.

In the Arc Wikimedia lab, the collector samples the runtime API every 10 seconds, Arc may hold the resulting telemetry row until its configured buffer age expires or the size threshold is reached, and the buffer dashboard refreshes every 15 seconds. Estimate the normal delay as the sum of those intervals. The lab's default 30-second buffer age gives roughly 55 seconds; a 5-second age gives roughly 30 seconds. This is an estimate, not a guarantee; errors and queueing can add time. The collector's `FLUSH_SECONDS` applies to source batches, not the runtime configuration sampling loop. Running `show` reads live process values and does not force this telemetry path. See the [Arc runtime buffer guide](https://github.com/Basekick-Labs/arc/blob/main/docs/runtime-ingest-buffer-config.md#observability-delay) for details.

## Related implementation

- `internal/commands/ingest.go` defines the `show`, `set`, and `reset` commands.
- `internal/client/runtime_ingest.go` implements the HTTP calls and client-side validation.
- Arc's API and persistence behavior are documented in the [Arc runtime ingest buffer guide](https://github.com/Basekick-Labs/arc/blob/main/docs/runtime-ingest-buffer-config.md).
