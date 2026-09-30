#!/usr/bin/env python3
"""Poll SafeDep endpoint Package Guard block events and sync them to CrowdStrike.

Reads malicious-package blocks and dependency-cooldown blocks from the SafeDep
Cloud API on an interval, then pushes each batch to a CrowdStrike SIEM HEC
(HTTP Event Collector) endpoint. Uses only the Python standard library.

If the CrowdStrike variables are unset, events are logged instead of sent, so
the sync can run on its own for testing.

Environment:
    SAFEDEP_TOKEN          required. OAuth token from `safedep auth token`.
    SAFEDEP_TENANT_ID      required. Tenant domain, e.g. your-company.safedep.io
    SAFEDEP_CLOUD_URL      optional. Default https://cloud.safedep.io
    CROWDSTRIKE_HEC_URL    optional. HEC collector URL, e.g.
                           https://<cloud>/services/collector. Unset = log only.
    CROWDSTRIKE_HEC_TOKEN  optional. HEC ingest token. Required with the URL.
    POLL_INTERVAL          optional. Seconds between cycles. Default 300.
    BACKFILL_HOURS         optional. First-run window in hours. Default 24.
"""

import json
import logging
import os
import time
import urllib.request
from datetime import datetime, timedelta, timezone

log = logging.getLogger("package-guard-sync")

CLOUD_URL = os.environ.get("SAFEDEP_CLOUD_URL", "https://cloud.safedep.io")
TOKEN = os.environ.get("SAFEDEP_TOKEN", "")
TENANT = os.environ.get("SAFEDEP_TENANT_ID", "")
HEC_URL = os.environ.get("CROWDSTRIKE_HEC_URL", "")
HEC_TOKEN = os.environ.get("CROWDSTRIKE_HEC_TOKEN", "")
POLL_INTERVAL = int(os.environ.get("POLL_INTERVAL", "300"))
BACKFILL_HOURS = int(os.environ.get("BACKFILL_HOURS", "24"))

CURSOR_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cursor.json")
USER_AGENT = "safedep-package-guard-sync"

METHOD = "/safedep.services.controltower.v1.EndpointManagementService/ListEndpointPackageGuardEvents"
FILTER = {
    "pmg": {
        "eventTypes": ["PMG_EVENT_TYPE_PACKAGE_DECISION"],
        "packageActions": ["PMG_PACKAGE_ACTION_BLOCKED", "PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED"],
    }
}


def rfc3339(when):
    return when.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def list_events(start, end, page_token):
    """Call the SafeDep Cloud API for one page of block events."""
    body = json.dumps({
        "filter": FILTER,
        "timeRange": {"start": rfc3339(start), "end": rfc3339(end)},
        "pagination": {"pageSize": 100, "sortOrder": "SORT_ORDER_ASCENDING", "pageToken": page_token},
    }).encode()
    request = urllib.request.Request(CLOUD_URL + METHOD, data=body, method="POST", headers={
        "Content-Type": "application/json",
        "Connect-Protocol-Version": "1",
        "Authorization": TOKEN,   # JWT, sent as-is (no "Bearer" prefix)
        "X-Tenant-ID": TENANT,
        "User-Agent": USER_AGENT,  # a custom User-Agent is required by the edge
    })
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def poll(since, end):
    """Drain every event in [since, end] across pages and return the list."""
    events, page_token = [], ""
    while True:
        response = list_events(since, end, page_token)
        events.extend(response.get("events", []))
        page_token = response.get("pagination", {}).get("nextPageToken", "")
        if not page_token:
            return events


def event_time(event):
    """Best-effort epoch seconds from the event timestamp, or None."""
    try:
        return datetime.fromisoformat(event.get("timestamp", "").replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


def hec_record(event):
    """Wrap one SafeDep event as an HEC record. time indexes by event time, not
    receive time, so backfilled events keep their real timestamp in the SIEM."""
    record = {"event": event, "sourcetype": "safedep:package-guard"}
    when = event_time(event)
    if when is not None:
        record["time"] = when
    return record


def send_to_hec(events):
    """Push events to the CrowdStrike SIEM HEC endpoint as one batch."""
    # HEC payload is whitespace (newline) delimited JSON records. It is sent as
    # text/plain, not application/json, because the concatenated objects are not
    # a single JSON document (per the LogScale HEC docs).
    body = "\n".join(json.dumps(hec_record(event)) for event in events).encode()
    # Run with DEBUG=1 to see the exact payload sent to CrowdStrike.
    log.debug("HEC POST %s payload:\n%s", HEC_URL, body.decode("utf-8"))
    request = urllib.request.Request(HEC_URL, data=body, method="POST", headers={
        "Authorization": "Bearer " + HEC_TOKEN,
        "Content-Type": "text/plain; charset=utf-8",
        "User-Agent": USER_AGENT,
    })
    with urllib.request.urlopen(request, timeout=30) as response:
        raw = response.read()
    # A 2xx response means the batch was accepted (urlopen raises on non-2xx).
    # Some HEC servers also return a Splunk-style {"code":N}; treat a non-zero
    # code as an error, but do not require the field (LogScale uses HTTP status).
    try:
        result = json.loads(raw)
    except json.JSONDecodeError:
        result = {}
    if isinstance(result, dict) and result.get("code", 0) != 0:
        raise RuntimeError("HEC rejected the batch: %s" % result)


def log_event(event):
    """Fallback when no HEC endpoint is set: print the event."""
    decision = event.get("pmgEvent", {}).get("packageDecision", {})
    version = decision.get("packageVersion", {})
    name = version.get("package", {}).get("name", "")
    log.info("%s %s %s@%s on %s",
             event.get("timestamp", ""), decision.get("action", ""),
             name, version.get("version", ""),
             event.get("endpointName") or event.get("endpointId", ""))


def sync(events):
    """Send events to CrowdStrike, or log them if no HEC endpoint is set."""
    if not events:
        return
    if HEC_URL and HEC_TOKEN:
        send_to_hec(events)
    else:
        for event in events:
            log_event(event)


def load_since():
    """Read the saved cursor, or fall back to the backfill window."""
    try:
        with open(CURSOR_FILE) as f:
            return datetime.fromisoformat(json.load(f)["since"])
    except (OSError, ValueError, KeyError):
        return datetime.now(timezone.utc) - timedelta(hours=BACKFILL_HOURS)


def save_since(since):
    """Persist the cursor with an atomic write."""
    tmp = CURSOR_FILE + ".tmp"
    with open(tmp, "w") as f:
        json.dump({"since": since.isoformat()}, f)
    os.replace(tmp, CURSOR_FILE)


def main():
    level = logging.DEBUG if os.environ.get("DEBUG") else logging.INFO
    logging.basicConfig(level=level, format="%(asctime)s %(message)s")
    if not TOKEN or not TENANT:
        raise SystemExit("set SAFEDEP_TOKEN and SAFEDEP_TENANT_ID")
    if BACKFILL_HOURS < 0:
        raise SystemExit("BACKFILL_HOURS must be >= 0")
    if HEC_URL and not HEC_TOKEN:
        raise SystemExit("CROWDSTRIKE_HEC_TOKEN is required when CROWDSTRIKE_HEC_URL is set")

    hec = bool(HEC_URL and HEC_TOKEN)
    log.info("starting: poll every %ss, backfill %sh, target %s",
             POLL_INTERVAL, BACKFILL_HOURS, HEC_URL if hec else "log only")
    since = load_since()
    while True:
        end = datetime.now(timezone.utc)
        try:
            log.info("cycle: polling events since %s", rfc3339(since))
            events = poll(since, end)
            sync(events)               # push to CrowdStrike, or log
            since = end                # advance the cursor only after a successful sync
            save_since(since)
            log.info("cycle: %d event(s) %s, next poll in %ss",
                     len(events), "sent to CrowdStrike HEC" if hec else "logged", POLL_INTERVAL)
        except Exception as err:       # transient error: log and retry next cycle
            log.warning("cycle: failed (%s), retrying in %ss", err, POLL_INTERVAL)
        time.sleep(POLL_INTERVAL)


if __name__ == "__main__":
    main()
