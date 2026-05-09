# Telemetry

Mnemos has an opt-in, default-off telemetry channel for the project's
North Star metric ("weekly active projects with ≥1 evidence-backed
query"). Nothing is sent unless an operator explicitly opts in **and**
configures a destination endpoint.

## Default state

Telemetry is **off**. The CLI never makes outbound network requests for
telemetry purposes unless both gates below are open.

| Gate                      | How to open                              | Default |
| ------------------------- | ---------------------------------------- | ------- |
| Operator opt-in           | `mnemos metrics --telemetry-opt-in` or `MNEMOS_TELEMETRY_OPTIN=true` | **off** |
| Endpoint configured       | `MNEMOS_TELEMETRY_ENDPOINT=<url>`        | unset   |

Either gate alone is a no-op. Sending requires both.

## What's sent

The payload is the JSON shape produced by `mnemos metrics --workspace`.
Inspect it locally before deciding whether to opt in:

```sh
mnemos metrics --workspace
```

Fields:

| Field                          | Description                                                       |
| ------------------------------ | ----------------------------------------------------------------- |
| `install_id`                   | Random 16-byte hex token, persisted under `XDG_DATA_HOME/mnemos/install_id`. Not derived from any user-identifying value. |
| `workspace_fingerprint`        | First 16 hex chars of SHA-256 of the resolved DB path. Lets the dashboard count distinct workspaces without seeing the path. |
| `as_of`                        | UTC timestamp of the snapshot.                                    |
| `version`, `go_version`, `os`, `arch` | Build metadata.                                            |
| `active_run_ids_7d`            | Distinct `run_id` count among events ingested in the past 7 days. |
| `events_7d`, `claims_7d`       | Aggregated counts in the past 7 days.                             |
| `evidence_backed_claims_7d`    | Claims created in the past 7d with citations or non-zero verify count. |
| `weekly_active`                | Boolean: ≥1 active run AND ≥1 evidence-backed claim in 7d.        |
| `total_claims`, `avg_trust_score`, `contested_claims` | Workspace-wide aggregates.            |
| `telemetry_opt_in`             | Mirrors the gate state — debuggable echo, never used to bypass.   |
| `telemetry_endpoint_configured`| Mirrors the gate state — debuggable echo, never used to bypass.   |

What is **not** in the payload: claim text, run names, file paths,
event content, embedding vectors, source input IDs, source documents,
agent identities, or any other contents from the underlying store.

## How to opt in

```sh
# 1. Inspect locally first
mnemos metrics --workspace

# 2. Set the destination endpoint
export MNEMOS_TELEMETRY_ENDPOINT=https://telemetry.mnemos.dev/v1/telemetry

# 3. Opt in (writes a marker file under the data dir)
mnemos metrics --telemetry-opt-in

# 4. Send a payload (one-shot — wire into your scheduler if you want
#    weekly aggregation)
mnemos metrics --workspace --telemetry-send
```

The endpoint receives an HTTP POST with `Content-Type: application/json`
and the payload above. A 2xx response is treated as success; 4xx/5xx
surface as a non-zero exit and an error to stderr.

## How to opt out

```sh
mnemos metrics --telemetry-opt-out
```

This removes the marker file. The env-based opt-in
(`MNEMOS_TELEMETRY_OPTIN`) is unaffected — clear it from your shell
environment if it is set.

## Rotating the install ID

Delete the install_id file:

```sh
rm "$XDG_DATA_HOME/mnemos/install_id"     # or ~/.local/share/mnemos/install_id
```

The next telemetry call generates a fresh ID.

## Implementation

Source lives in `cmd/mnemos/telemetry.go`. The CLI handler
(`handleWorkspaceMetrics`) and the `--telemetry-*` flag parsing are in
`cmd/mnemos/main.go::handleMetrics`. The two gates are enforced inside
`sendTelemetry`; both must hold for an HTTP request to be issued.
