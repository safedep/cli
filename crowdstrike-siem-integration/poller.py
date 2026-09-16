#!/usr/bin/env python3
"""Poll SafeDep endpoint package-guard block events and log them.

This is a proof of concept. It reads malicious-package block events and
dependency-cooldown block events from the SafeDep control plane, on an interval,
and writes each new event to the log. A later version will forward each event to
the CrowdStrike SIEM (see handle_event).

It uses the Connect protocol (JSON over HTTPS POST), so it needs no external
packages. The Python standard library is enough.

Configuration comes from environment variables:

    SAFEDEP_TOKEN          required. OAuth access token. Get it with
                           `safedep auth token`.
    SAFEDEP_TENANT_ID      required. Tenant domain, e.g.
                           default-team.safedep-io.safedep.io
    SAFEDEP_CLOUD_URL      optional. Default https://cloud.safedep.io
    POLL_INTERVAL_SECONDS  optional. Default 300.
    BACKFILL_HOURS         optional. First-run window in hours. Default 0
                           (start from now).
    PAGE_SIZE              optional. Default 100 (the service caps it).
    HTTP_TIMEOUT_SECONDS   optional. Default 30.

Run:

    export SAFEDEP_TOKEN=$(safedep auth token)
    export SAFEDEP_TENANT_ID=your-tenant.safedep.io
    python3 poller.py
"""

from __future__ import annotations

import json
import logging
import os
import re
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone

log = logging.getLogger("safedep-crowdstrike-poller")

# ---- console styling: stdlib only. Color on a TTY, plain text otherwise.
# NO_COLOR disables color. FORCE_COLOR enables it even when piped.
_USE_COLOR = (os.environ.get("NO_COLOR") is None) and (
    sys.stderr.isatty() or bool(os.environ.get("FORCE_COLOR"))
)
_MALWARE_BADGE = "1;97;41"  # bold white on red
_COOLDOWN_BADGE = "1;30;43"  # bold black on yellow


def _sgr(codes: str, text: str) -> str:
    return f"\033[{codes}m{text}\033[0m" if _USE_COLOR else text


def _dim(text: str) -> str:
    return _sgr("2", text)


def _bold(text: str) -> str:
    return _sgr("1", text)


def _badge(text: str, codes: str) -> str:
    return _sgr(codes, f" {text} ") if _USE_COLOR else f"[{text}]"


class _ConsoleFormatter(logging.Formatter):
    """Compact console output: a dim HH:MM:SS, then the message. Warnings and
    errors are tinted. The message may carry its own color."""

    def format(self, record: logging.LogRecord) -> str:
        stamp = _dim(datetime.fromtimestamp(record.created).strftime("%H:%M:%S"))
        message = record.getMessage()
        if record.levelno >= logging.ERROR:
            message = _sgr("31", message)
        elif record.levelno >= logging.WARNING:
            message = _sgr("33", message)
        return f"{stamp}  {message}"

# Control-plane Connect endpoint. The path is /<fully-qualified-service>/<method>.
_SERVICE = "safedep.services.controltower.v1.EndpointManagementService"
_METHOD = "ListEndpointPackageGuardEvents"

# Server-side filter: only package-decision events, only the two block actions.
_FILTER = {
    "pmg": {
        "eventTypes": ["PMG_EVENT_TYPE_PACKAGE_DECISION"],
        "packageActions": [
            "PMG_PACKAGE_ACTION_BLOCKED",
            "PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED",
        ],
    }
}

_ACTION_BLOCKED = "PMG_PACKAGE_ACTION_BLOCKED"
_ACTION_COOLDOWN_BLOCKED = "PMG_PACKAGE_ACTION_COOLDOWN_BLOCKED"

# Cursor is persisted next to this script, so the position survives restarts
# regardless of the working directory.
CURSOR_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cursor.json")


class PollError(Exception):
    """A transient failure of one poll cycle. The loop logs it and retries."""


@dataclass(frozen=True)
class Config:
    token: str
    tenant: str
    base_url: str
    poll_interval: float
    backfill: timedelta
    page_size: int
    timeout: float

    @property
    def url(self) -> str:
        return f"{self.base_url.rstrip('/')}/{_SERVICE}/{_METHOD}"

    @classmethod
    def from_env(cls) -> "Config":
        token = os.environ.get("SAFEDEP_TOKEN", "").strip()
        tenant = os.environ.get("SAFEDEP_TENANT_ID", "").strip()
        missing = [n for n, v in (("SAFEDEP_TOKEN", token), ("SAFEDEP_TENANT_ID", tenant)) if not v]
        if missing:
            raise ValueError(
                "missing required environment variable(s): "
                + ", ".join(missing)
                + ". Set SAFEDEP_TOKEN=$(safedep auth token) and SAFEDEP_TENANT_ID=<tenant>."
            )
        return cls(
            token=token,
            tenant=tenant,
            base_url=os.environ.get("SAFEDEP_CLOUD_URL", "https://cloud.safedep.io").strip(),
            poll_interval=_env_float("POLL_INTERVAL_SECONDS", 300.0),
            backfill=timedelta(hours=_env_float("BACKFILL_HOURS", 0.0)),
            page_size=int(_env_float("PAGE_SIZE", 100.0)),
            timeout=_env_float("HTTP_TIMEOUT_SECONDS", 30.0),
        )


@dataclass(frozen=True)
class Cooldown:
    publish_date: str
    cooldown_days: int
    days_since_publish: int
    days_remaining: int

    @classmethod
    def from_json(cls, cd: dict) -> "Cooldown":
        return cls(
            publish_date=cd.get("publishDate", ""),
            cooldown_days=int(cd.get("cooldownDays", 0)),
            days_since_publish=int(cd.get("daysSincePublish", 0)),
            days_remaining=int(cd.get("daysRemaining", 0)),
        )


@dataclass(frozen=True)
class BlockEvent:
    event_id: str
    timestamp: str  # RFC3339, as returned by the server
    endpoint_id: str
    endpoint_name: str
    tool_name: str
    action: str  # PMG_PACKAGE_ACTION_*
    ecosystem: str
    package: str
    version: str
    is_malware: bool
    is_verified: bool
    analysis_id: str
    cooldown: "Cooldown | None"
    raw: dict  # the untouched event, for the future SIEM payload

    @classmethod
    def from_json(cls, ev: dict) -> "BlockEvent | None":
        decision = (ev.get("pmgEvent") or {}).get("packageDecision")
        if not decision:
            return None
        pv = decision.get("packageVersion") or {}
        pkg = pv.get("package") or {}
        cd = decision.get("cooldown")
        return cls(
            event_id=ev.get("eventId", ""),
            timestamp=ev.get("timestamp", ""),
            endpoint_id=ev.get("endpointId", ""),
            endpoint_name=ev.get("endpointName", ""),
            tool_name=ev.get("toolName", ""),
            action=decision.get("action", "PMG_PACKAGE_ACTION_UNSPECIFIED"),
            ecosystem=pkg.get("ecosystem", ""),
            package=pkg.get("name", ""),
            version=pv.get("version", ""),
            is_malware=bool(decision.get("isMalware", False)),
            is_verified=bool(decision.get("isVerified", False)),
            analysis_id=decision.get("analysisId", ""),
            cooldown=Cooldown.from_json(cd) if cd else None,
            raw=ev,
        )


@dataclass
class Cursor:
    """Cross-cycle position. Events are immutable, so a timestamp watermark is a
    correct cursor. last_seen_ids holds the ids at exactly last_seen, to skip the
    boundary if the server treats TimeRange.start as inclusive.

    It persists to a local JSON file, so a restart resumes where it stopped.
    Delete that file to re-read from the backfill window.
    """

    last_seen: "datetime | None" = None
    last_seen_ids: set = field(default_factory=set)

    @classmethod
    def load(cls, path: str) -> "Cursor":
        try:
            with open(path, "r", encoding="utf-8") as handle:
                data = json.load(handle)
        except FileNotFoundError:
            return cls()
        except (OSError, ValueError) as exc:
            log.warning("Cursor file %s is unreadable (%s). Starting fresh.", path, exc)
            return cls()
        last_seen = data.get("last_seen")
        return cls(
            last_seen=parse_rfc3339(last_seen) if last_seen else None,
            last_seen_ids=set(data.get("last_seen_ids") or []),
        )

    def save(self, path: str) -> None:
        data = {
            "last_seen": to_rfc3339(self.last_seen) if self.last_seen else None,
            "last_seen_ids": sorted(self.last_seen_ids),
        }
        tmp = path + ".tmp"
        try:
            with open(tmp, "w", encoding="utf-8") as handle:
                json.dump(data, handle, indent=2)
            os.replace(tmp, path)  # atomic: never leave a half-written cursor
        except OSError as exc:
            log.warning("Could not save cursor to %s: %s", path, exc)


def _env_float(name: str, default: float) -> float:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        return float(raw)
    except ValueError as exc:
        raise ValueError(f"{name} must be a number, got {raw!r}") from exc


def _headers(cfg: Config) -> dict:
    return {
        "Content-Type": "application/json",
        "Connect-Protocol-Version": "1",
        # The control plane expects the raw JWT, with no "Bearer " prefix.
        "Authorization": cfg.token,
        "x-tenant-id": cfg.tenant,
        # Set an explicit User-Agent. The default urllib agent is blocked by the
        # Cloudflare edge in front of the control plane (HTTP 403, error 1010).
        "User-Agent": "safedep-crowdstrike-poller/0.1",
    }


def _post(cfg: Config, payload: dict) -> dict:
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(cfg.url, data=body, headers=_headers(cfg), method="POST")
    try:
        with urllib.request.urlopen(req, timeout=cfg.timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", "replace")
        raise PollError(f"HTTP {exc.code}: {detail}") from exc
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise PollError(f"request failed: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise PollError(f"invalid JSON response: {exc}") from exc


_FRACTION = re.compile(r"\.(\d+)")


def parse_rfc3339(value: str) -> datetime:
    """Parse an RFC3339 timestamp. Handles a trailing Z and trims fractional
    seconds to microseconds, which is the most datetime supports."""
    text = value.strip()
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    text = _FRACTION.sub(lambda m: "." + m.group(1)[:6], text, count=1)
    return datetime.fromisoformat(text)


def to_rfc3339(when: datetime) -> str:
    return when.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def _ecosystem(value: str) -> str:
    return value[len("ECOSYSTEM_") :].lower() if value.startswith("ECOSYSTEM_") else value.lower()


def handle_event(event: BlockEvent) -> None:
    """Handle one new block event. For now it logs. A later version will forward
    the event to the CrowdStrike SIEM ingest API here (use event.raw)."""
    package = f"{event.package}@{event.version}" if event.version else event.package
    endpoint = event.endpoint_name or event.endpoint_id
    ecosystem = _dim(f"({_ecosystem(event.ecosystem)})")
    if event.action == _ACTION_COOLDOWN_BLOCKED:
        badge = _badge("COOLDOWN", _COOLDOWN_BADGE)
        days = event.cooldown.days_remaining if event.cooldown else 0
        detail = _sgr("33", f"{days}d cooldown left")
    else:
        badge = _badge("MALWARE", _MALWARE_BADGE)
        detail = _sgr("31", "malware")
        if not event.is_verified:
            detail += _dim(" (unverified)")
    log.info("%s %s %s  %s %s", badge, _bold(package), ecosystem, detail, _dim(f"on {endpoint}"))
    # TODO(stage 2): POST event.raw to the CrowdStrike SIEM ingest API here.


def poll_once(cfg: Config, cursor: Cursor) -> int:
    """Drain every new event and advance the cursor. Return the count handled."""
    now = datetime.now(timezone.utc)
    start = cursor.last_seen or (now - cfg.backfill)
    end = now
    if start >= end:
        # Fresh start from now: the window is empty. Nudge end so start < end,
        # which the API requires.
        end = start + timedelta(seconds=1)

    new_max = cursor.last_seen
    new_max_ids: set = set()
    handled = 0
    page_token = ""

    while True:
        pagination = {"pageSize": cfg.page_size, "sortOrder": "SORT_ORDER_ASCENDING"}
        if page_token:
            pagination["pageToken"] = page_token
        payload = {
            "filter": _FILTER,
            "timeRange": {"start": to_rfc3339(start), "end": to_rfc3339(end)},
            "pagination": pagination,
        }

        resp = _post(cfg, payload)

        for raw in resp.get("events", []) or []:
            event = BlockEvent.from_json(raw)
            if event is None or event.event_id in cursor.last_seen_ids:
                continue
            handle_event(event)
            handled += 1
            if not event.timestamp:
                continue
            ts = parse_rfc3339(event.timestamp)
            if new_max is None or ts > new_max:
                new_max, new_max_ids = ts, {event.event_id}
            elif ts == new_max:
                new_max_ids.add(event.event_id)

        page_token = (resp.get("pagination") or {}).get("nextPageToken", "")
        if not page_token:
            break

    if new_max is not None and (cursor.last_seen is None or new_max > cursor.last_seen):
        cursor.last_seen, cursor.last_seen_ids = new_max, new_max_ids
    elif cursor.last_seen is None:
        # First cycle saw nothing. Anchor so the window does not slide forward by
        # one interval each cycle.
        cursor.last_seen = start
    return handled


def main() -> int:
    handler = logging.StreamHandler()
    handler.setFormatter(_ConsoleFormatter())
    root = logging.getLogger()
    root.setLevel(logging.INFO)
    root.addHandler(handler)

    try:
        cfg = Config.from_env()
    except ValueError as exc:
        log.error("%s", exc)
        return 2

    cursor = Cursor.load(CURSOR_PATH)
    log.info("%s", _bold(_sgr("36", "SafeDep endpoint block event poller")))
    log.info("%s", _dim(f"tenant {cfg.tenant}  |  every {int(cfg.poll_interval)}s  |  backfill {cfg.backfill}"))
    if cursor.last_seen:
        log.info("%s", _dim(f"resuming from {to_rfc3339(cursor.last_seen)}"))
    else:
        log.info("%s", _dim("no saved cursor; first cycle reads the backfill window"))

    while True:
        try:
            count = poll_once(cfg, cursor)
            cursor.save(CURSOR_PATH)
            if count:
                log.info("%s %s %s", _dim("cycle done"), _sgr("32", f"{count} new event(s)"), _dim(f"next in {int(cfg.poll_interval)}s"))
            else:
                log.info("%s", _dim(f"cycle done  |  0 new  |  next in {int(cfg.poll_interval)}s"))
        except PollError as exc:
            log.warning("cycle error: %s", exc)
        try:
            time.sleep(cfg.poll_interval)
        except KeyboardInterrupt:
            log.info("%s", _dim("stopped"))
            return 0


if __name__ == "__main__":
    sys.exit(main())
