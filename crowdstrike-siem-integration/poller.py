#!/usr/bin/env python3
"""Poll SafeDep endpoint Package Guard block events and print them.

Reads malicious-package blocks and dependency-cooldown blocks from the SafeDep
Cloud API on an interval. Uses only the Python standard library.

Environment:
    SAFEDEP_TOKEN      required. OAuth token from `safedep auth token`.
    SAFEDEP_TENANT_ID  required. Tenant domain, e.g. your-company.safedep.io
    SAFEDEP_CLOUD_URL  optional. Default https://cloud.safedep.io
    POLL_INTERVAL      optional. Seconds between cycles. Default 300.
    BACKFILL_HOURS     optional. First-run window in hours. Default 24.
"""

import json
import logging
import os
import time
import urllib.request
from datetime import datetime, timedelta, timezone

log = logging.getLogger("package-guard-poller")

CLOUD_URL = os.environ.get("SAFEDEP_CLOUD_URL", "https://cloud.safedep.io")
TOKEN = os.environ.get("SAFEDEP_TOKEN", "")
TENANT = os.environ.get("SAFEDEP_TENANT_ID", "")
POLL_INTERVAL = int(os.environ.get("POLL_INTERVAL", "300"))
BACKFILL_HOURS = int(os.environ.get("BACKFILL_HOURS", "24"))

# Cursor file next to this script, so restarts resume where they stopped.
CURSOR_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cursor.json")

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
    """Call the Cloud API for one page of block events."""
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
        "User-Agent": "safedep-package-guard-poller",  # a custom User-Agent is required by the edge
    })
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def handle(event):
    """Handle one block event. Send it to your SIEM here."""
    # Use .get() throughout: a malformed event must not raise, or it would
    # abort the cycle and block the cursor from ever advancing past it.
    decision = event.get("pmgEvent", {}).get("packageDecision", {})
    version = decision.get("packageVersion", {})
    name = version.get("package", {}).get("name", "")
    log.info("%s %s %s@%s on %s",
             event.get("timestamp", ""), decision.get("action", ""),
             name, version.get("version", ""),
             event.get("endpointName") or event.get("endpointId", ""))


def poll(since, end):
    """Drain every event in [since, end] and return how many were handled."""
    count, page_token = 0, ""
    while True:
        response = list_events(since, end, page_token)
        for event in response.get("events", []):
            handle(event)
            count += 1
        page_token = response.get("pagination", {}).get("nextPageToken", "")
        if not page_token:
            return count


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
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")
    if not TOKEN or not TENANT:
        raise SystemExit("set SAFEDEP_TOKEN and SAFEDEP_TENANT_ID")
    if BACKFILL_HOURS < 0:
        raise SystemExit("BACKFILL_HOURS must be >= 0")

    since = load_since()
    while True:
        end = datetime.now(timezone.utc)
        try:
            count = poll(since, end)
            since = end            # next cycle reads only newer events
            save_since(since)      # resume from here after a restart
            log.info("cycle done: %d event(s)", count)
        except Exception as err:   # transient error: log and retry next cycle
            log.warning("cycle error: %s", err)
        time.sleep(POLL_INTERVAL)


if __name__ == "__main__":
    main()
