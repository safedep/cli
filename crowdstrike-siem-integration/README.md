# SafeDep to CrowdStrike endpoint block event poller (POC)

A single Python script that polls SafeDep endpoint package-guard **block events**
and logs them. Two signals, treated the same:

- Malicious package blocks (`PMG_PACKAGE_ACTION_BLOCKED`)
- Dependency cooldown blocks (`PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED`)

This is a proof of concept. It only logs the events for now. A later version will
forward each event to the CrowdStrike SIEM (see `handle_event` in `poller.py`).

## Requirements

- Python 3.8 or later.
- No external packages. It uses the standard library only.

The script calls the SafeDep control plane over the Connect protocol (JSON over
HTTPS POST), so it does not need gRPC or protobuf.

## Configuration

Set these environment variables:

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFEDEP_TOKEN` | yes | - | OAuth access token. Get it with `safedep auth token`. |
| `SAFEDEP_TENANT_ID` | yes | - | Tenant domain, e.g. `default-team.safedep-io.safedep.io`. |
| `SAFEDEP_CLOUD_URL` | no | `https://cloud.safedep.io` | Control-plane base URL. |
| `POLL_INTERVAL_SECONDS` | no | `300` | Sleep between cycles. |
| `BACKFILL_HOURS` | no | `0` | First-run window. `0` starts from now. |
| `PAGE_SIZE` | no | `100` | Page size. The service caps it. |
| `HTTP_TIMEOUT_SECONDS` | no | `30` | Per-request timeout. |

## Run

```bash
export SAFEDEP_TOKEN=$(safedep auth token)
export SAFEDEP_TENANT_ID=your-tenant.safedep.io
export BACKFILL_HOURS=168   # optional: read the last 7 days on the first cycle

python3 poller.py
```

The access token is short-lived. For a long run, restart the poller with a fresh
token, or set `SAFEDEP_TOKEN` from `safedep auth token` again.

## Notes

- The cursor is saved to `cursor.json` next to the script. A restart resumes from
  it. Delete `cursor.json` to re-read from the backfill window.
- The poller pulls only new events each cycle. It moves `TimeRange.start` to the
  timestamp of the last event, and skips events at that timestamp that it already
  handled.
