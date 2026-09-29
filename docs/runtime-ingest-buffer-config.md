# Runtime ingest buffer commands

`arcli` provides commands to inspect and change Arc's runtime ingest buffer limits through the administrator API. Arc applies a change to the current process and persists it so the values return after an Arc restart.

## Commands

```sh
# Show the effective values
arcli ingest buffer show

# Change either limit or both
arcli ingest buffer set --max-buffer-size 200000
arcli ingest buffer set --max-buffer-age-ms 30000
arcli ingest buffer set --max-buffer-size 200000 --max-buffer-age-ms 30000

# Remove the saved override and restore Arc's startup values
arcli ingest buffer reset
```

`set` requires at least one flag. An omitted setting keeps its current effective value. Supplied values must be greater than zero; Arc also rejects buffer ages too large to represent as a duration. `reset` calls Arc's `DELETE /api/v1/config/runtime/ingest` endpoint; it does not restart Arc.

## Connection and permissions

The commands use the same connection selection as other `arcli` commands: a saved connection profile or the `ARC_ENDPOINT` and `ARC_TOKEN` environment variables. When Arc authentication is enabled, the token must have administrator privileges.

For the local Compose lab, run:

```sh
docker compose exec arcli arcli ingest buffer show
docker compose exec arcli arcli ingest buffer set --max-buffer-size 200000 --max-buffer-age-ms 30000
docker compose exec arcli arcli ingest buffer reset
```

The settings are process-local. In a multi-node deployment, target each Arc node separately; `arcli` does not fan out or replicate the request.

## Output

The default table output includes the effective `max_buffer_size`, `max_buffer_age_ms`, scope, persistence state, and source. Use `--output json` (or `-o json`) for scripts:

```sh
arcli ingest buffer show --output json
arcli ingest buffer set --max-buffer-age-ms 30000 --output json
arcli ingest buffer reset --output json
```

Arc reports `source: "persistent_override"` while an override exists and `source: "startup_config"` after reset. `persistent` reports whether Arc has a saved override.

## Related implementation

- `internal/commands/ingest.go` defines the `show`, `set`, and `reset` commands.
- `internal/client/runtime_ingest.go` implements the HTTP calls and client-side validation.
- Arc's API and persistence behavior are documented in the [Arc runtime ingest buffer guide](https://github.com/jallegri/arc/blob/codex/persist-ingest-buffer/docs/runtime-ingest-buffer-config.md).
