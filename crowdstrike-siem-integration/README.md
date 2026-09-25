# SafeDep to CrowdStrike SIEM sync (POC)

`sync.py` polls SafeDep endpoint Package Guard **block events** and pushes them to
a CrowdStrike SIEM HEC (HTTP Event Collector) endpoint. It does the full job end
to end: poll, then sync. Two signals are covered:

- Malicious package blocks (`PMG_PACKAGE_ACTION_BLOCKED`)
- Dependency cooldown blocks (`PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED`)

If the CrowdStrike variables are not set, `sync.py` logs the events instead of
sending them, so you can run it on its own.

## Requirements

- Python 3.8 or later. No external packages (standard library only).
- The [safedep CLI](https://github.com/safedep/cli), logged in, to mint the token.

The script calls the SafeDep Cloud API over the Connect protocol (JSON over HTTPS
POST), so it needs no gRPC or protobuf library.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `SAFEDEP_TOKEN` | yes | - | SafeDep OAuth token. Get it with `safedep auth token`. |
| `SAFEDEP_TENANT_ID` | yes | - | Tenant domain, e.g. `your-company.safedep.io`. |
| `SAFEDEP_CLOUD_URL` | no | `https://cloud.safedep.io` | SafeDep Cloud base URL. |
| `CROWDSTRIKE_HEC_URL` | no | (unset) | HEC collector URL, e.g. `https://<cloud>/services/collector`. Unset means log only. |
| `CROWDSTRIKE_HEC_TOKEN` | no | (unset) | HEC ingest token. Required when `CROWDSTRIKE_HEC_URL` is set. |
| `POLL_INTERVAL` | no | `300` | Seconds between poll cycles. |
| `BACKFILL_HOURS` | no | `24` | First-run window in hours. |

## Get a token and tenant

```bash
safedep auth login                       # once, if not already logged in
safedep auth status                      # shows your tenant domain
export SAFEDEP_TENANT_ID=your-company.safedep.io
export SAFEDEP_TOKEN=$(safedep auth token)
```

The token is short-lived. For a long run, refresh `SAFEDEP_TOKEN` with
`safedep auth token` again before it expires.

## Run against CrowdStrike

```bash
export SAFEDEP_TOKEN=$(safedep auth token)
export SAFEDEP_TENANT_ID=your-company.safedep.io
export CROWDSTRIKE_HEC_URL=https://<your-cloud>/services/collector
export CROWDSTRIKE_HEC_TOKEN=<your-hec-ingest-token>

python3 sync.py
```

## Run without CrowdStrike (log only)

Leave the CrowdStrike variables unset to poll and print the events:

```bash
export SAFEDEP_TOKEN=$(safedep auth token)
export SAFEDEP_TENANT_ID=your-company.safedep.io
python3 sync.py
```

## Test with the fake HEC server

`fake-siem/` is a throwaway Go server that mimics the CrowdStrike HEC endpoint, so
you can test the full pipeline without CrowdStrike access.

```bash
# 1. Start the fake HEC server (listens on :8088).
FAKE_HEC_TOKEN=test-token go run ./fake-siem

# 2. In another shell, run the sync against it.
export SAFEDEP_TOKEN=$(safedep auth token)
export SAFEDEP_TENANT_ID=your-company.safedep.io
export CROWDSTRIKE_HEC_URL=http://localhost:8088/services/collector
export CROWDSTRIKE_HEC_TOKEN=test-token
export BACKFILL_HOURS=168     # read the last 7 days on the first cycle
python3 sync.py
```

The fake server prints each event it receives and the batch count. When you get
real CrowdStrike access, set `CROWDSTRIKE_HEC_URL` and `CROWDSTRIKE_HEC_TOKEN` to
the real values. No code change is needed.

## How it works

- **Poll.** Each cycle reads new block events from the SafeDep Cloud API in the
  window `[last cursor, now]`, following pagination.
- **Sync.** The batch is sent to the HEC endpoint as newline-delimited records.
- **Cursor.** The position is saved to `cursor.json` next to the script, so a
  restart resumes where it stopped. Delete `cursor.json` to re-read from the
  backfill window.
- **At least once.** The cursor advances only after a successful sync. If a sync
  fails, the same window is retried on the next cycle.
