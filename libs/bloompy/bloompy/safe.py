"""The Safe class — the primary user-facing API for bloompy."""
from __future__ import annotations

import logging
import os
from typing import Callable, TypeVar

from .client import LoomLockerClient
from .config import LockerConfig

logger = logging.getLogger(__name__)
T = TypeVar("T")


class Safe:
    """Fluent API for safely loading secrets in loomlocker-protected projects.

    Basic usage::

        from bloompy import Safe

        # Simple: unlock → run → autolock
        Safe().unlock().execute(lambda: load_dotenv()).autolock()

        # Explicit password (falls back to LOOM_SESSION_PASSWORD env var)
        Safe().unlock("my-password").execute(load_dotenv).autolock()

    If loomlocker is not running, all operations are transparent no-ops —
    the ``execute`` function always runs.
    """

    def __init__(
        self,
        config: LockerConfig | None = None,
        client: LoomLockerClient | None = None,
    ) -> None:
        self._cfg = config or LockerConfig.from_env()
        self._client = client or LoomLockerClient(self._cfg)
        self._unlocked = False
        self._silent = False

    # ── Fluent API ────────────────────────────────────────────────────────────

    def silent(self) -> "Safe":
        """Suppress all warnings from this Safe (debug logging is unaffected)."""
        self._silent = True
        return self

    def _warn(self, message: str, *args) -> None:
        if not self._silent:
            logger.warning(message, *args)

    def unlock(self, password: str | None = None) -> "Safe":
        """Unlock secrets if loomlocker is running and they are locked.

        Password precedence:
        1. ``password`` argument
        2. ``LOOM_SESSION_PASSWORD`` environment variable
        3. No-op (server not running or already unlocked)
        """
        if not self._client.is_running():
            logger.debug("bloompy: loomlocker not running — skipping unlock")
            return self

        if not self._client.is_locked():
            logger.debug("bloompy: secrets already unlocked")
            self._unlocked = True
            return self

        pwd = password or os.getenv("LOOM_SESSION_PASSWORD", "")
        if not pwd:
            self._warn(
                "bloompy: secrets are locked but no password provided. "
                "Set LOOM_SESSION_PASSWORD or pass password= to unlock()."
            )
            return self

        if self._client.unlock(pwd):
            logger.debug("bloompy: secrets unlocked (auto-relock in %ds)",
                         self._cfg.unlock_duration_seconds)
            self._unlocked = True
        else:
            self._warn("bloompy: unlock failed — check password")

        return self

    def execute(self, fn: Callable[[], T]) -> "Safe":
        """Run *fn* and return self for chaining. Always called regardless of lock state."""
        fn()
        return self

    def autolock(self) -> "Safe":
        """Signal loomlocker that startup is complete → trigger immediate relock.

        If loomlocker is not running or unlock() was not called, this is a no-op.
        """
        if self._unlocked and self._client.is_running():
            self._client.autolock()
            self._unlocked = False
            logger.debug("bloompy: autolock requested")
        return self

    # ── Context manager support ───────────────────────────────────────────────

    def __enter__(self) -> "Safe":
        return self.unlock()

    def __exit__(self, *_) -> None:
        self.autolock()
