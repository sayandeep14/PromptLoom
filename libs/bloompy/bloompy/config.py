"""Reads loomlocker connection settings from .loom.config or environment variables."""
from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class LockerConfig:
    host: str = "http://localhost"
    port: str = "8053"
    unlock_duration_seconds: int = 10

    @property
    def base_url(self) -> str:
        return f"{self.host}:{self.port}/api"

    @classmethod
    def from_env(cls) -> "LockerConfig":
        """Load config from environment variables, then fall back to .loom.config."""
        cfg = cls._from_loom_config() or cls()
        # Env vars override file config.
        if host := os.getenv("LOOM_HOST"):
            cfg.host = host
        if port := os.getenv("LOOM_PORT"):
            cfg.port = port
        return cfg

    @classmethod
    def _from_loom_config(cls) -> "LockerConfig | None":
        """Search current dir and parents for .loom.config."""
        path = _find_config()
        if path is None:
            return None
        try:
            data = json.loads(path.read_text())
            lc = data.get("loomlocker", {})
            return cls(
                host=lc.get("lockhost", "http://localhost"),
                port=str(lc.get("port", "8053")),
                unlock_duration_seconds=int(lc.get("unlock_duration_seconds", 10)),
            )
        except Exception:
            return None


def _find_config(start: Path | None = None) -> Path | None:
    d = Path(start or os.getcwd()).resolve()
    while True:
        candidate = d / ".loom.config"
        if candidate.exists():
            return candidate
        parent = d.parent
        if parent == d:
            return None
        d = parent
