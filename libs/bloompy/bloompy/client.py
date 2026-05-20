"""HTTP client that talks to a running loomlocker server."""
from __future__ import annotations

import logging
from typing import Any

try:
    import requests as _requests
    _HAS_REQUESTS = True
except ImportError:
    _HAS_REQUESTS = False

from .config import LockerConfig

logger = logging.getLogger(__name__)


class LoomLockerClient:
    """Thin HTTP client for the loomlocker server.

    All methods are silent on network errors — if loomlocker is not running,
    they are no-ops. This ensures application startup never fails because of
    loomlocker availability.
    """

    def __init__(self, config: LockerConfig | None = None):
        self._cfg = config or LockerConfig.from_env()

    # ── Connectivity ──────────────────────────────────────────────────────────

    def is_running(self) -> bool:
        """Return True if the loomlocker server is reachable."""
        try:
            resp = self._get("/ping", timeout=2)
            return resp is not None and resp.get("status") == "ok"
        except Exception:
            return False

    def is_locked(self) -> bool:
        """Return the current lock state. Returns False if unreachable."""
        try:
            resp = self._get("/ping", timeout=2)
            return bool(resp and resp.get("locked", False))
        except Exception:
            return False

    # ── Actions ───────────────────────────────────────────────────────────────

    def unlock(self, password: str) -> bool:
        """Send an unlock request. Returns True on success, False otherwise."""
        try:
            resp = self._post("/unlock", {"password": password}, timeout=5)
            return resp is not None and "error" not in resp
        except Exception as e:
            logger.debug("bloompy: unlock request failed: %s", e)
            return False

    def lock(self) -> bool:
        """Request an immediate lock. Returns True on success."""
        try:
            resp = self._post("/lock", {}, timeout=3)
            return resp is not None
        except Exception:
            return False

    def autolock(self) -> bool:
        """Signal loomlocker that startup is complete — triggers immediate relock."""
        try:
            resp = self._post("/autolock", {}, timeout=3)
            return resp is not None
        except Exception:
            return False

    # ── Internal ──────────────────────────────────────────────────────────────

    def _get(self, path: str, timeout: int = 5) -> dict[str, Any] | None:
        if not _HAS_REQUESTS:
            return self._get_urllib(path, timeout)
        resp = _requests.get(self._cfg.base_url + path, timeout=timeout)
        resp.raise_for_status()
        return resp.json()

    def _post(self, path: str, body: dict, timeout: int = 5) -> dict[str, Any] | None:
        if not _HAS_REQUESTS:
            return self._post_urllib(path, body, timeout)
        resp = _requests.post(
            self._cfg.base_url + path,
            json=body,
            timeout=timeout,
        )
        resp.raise_for_status()
        return resp.json()

    # Fallback using stdlib urllib (no external dependencies).
    def _get_urllib(self, path: str, timeout: int) -> dict[str, Any] | None:
        import json
        import urllib.request
        with urllib.request.urlopen(self._cfg.base_url + path, timeout=timeout) as r:
            return json.loads(r.read())

    def _post_urllib(self, path: str, body: dict, timeout: int) -> dict[str, Any] | None:
        import json
        import urllib.request
        data = json.dumps(body).encode()
        req = urllib.request.Request(
            self._cfg.base_url + path,
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read())
