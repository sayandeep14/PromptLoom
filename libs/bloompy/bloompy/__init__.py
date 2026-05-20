"""
bloompy — Python client for LoomLocker.

Provides the Safe class for safely loading secrets in applications that
use loomlocker for session-scoped secret protection.

Usage::

    from bloompy import Safe

    s = Safe()
    s.unlock().execute(lambda: load_dotenv()).autolock()

    app.run()
"""

from .safe import Safe
from .client import LoomLockerClient
from .config import LockerConfig

__all__ = ["Safe", "LoomLockerClient", "LockerConfig"]
__version__ = "0.1.0"
