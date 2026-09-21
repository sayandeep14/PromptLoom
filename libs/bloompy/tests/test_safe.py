import logging
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from bloompy import Safe  # noqa: E402
from bloompy.config import LockerConfig  # noqa: E402


class FakeClient:
    """Stands in for LoomLockerClient; records what the library asked the server to do."""

    def __init__(self, running=True, locked=True, password="pw"):
        self.running, self.locked, self.password = running, locked, password
        self.calls = []

    def is_running(self):
        return self.running

    def is_locked(self):
        return self.locked

    def unlock(self, pwd):
        self.calls.append(("unlock", pwd))
        if pwd == self.password:
            self.locked = False
            return True
        return False

    def autolock(self):
        self.calls.append(("autolock",))
        self.locked = True


def safe(client, **kw):
    return Safe(config=LockerConfig.from_env(), client=client, **kw)


class SafeTests(unittest.TestCase):
    def setUp(self):
        self._old = os.environ.pop("LOOM_SESSION_PASSWORD", None)

    def tearDown(self):
        os.environ.pop("LOOM_SESSION_PASSWORD", None)
        if self._old is not None:
            os.environ["LOOM_SESSION_PASSWORD"] = self._old

    def test_unlock_execute_autolock(self):
        c = FakeClient()
        seen = []
        safe(c).unlock("pw").execute(lambda: seen.append(c.locked)).autolock()
        self.assertEqual(seen, [False], "the callback must run while the secrets are unlocked")
        self.assertEqual(c.calls, [("unlock", "pw"), ("autolock",)])
        self.assertTrue(c.locked)

    def test_password_from_environment(self):
        os.environ["LOOM_SESSION_PASSWORD"] = "pw"
        c = FakeClient()
        safe(c).unlock()
        self.assertFalse(c.locked)

    def test_argument_beats_environment(self):
        os.environ["LOOM_SESSION_PASSWORD"] = "wrong"
        c = FakeClient()
        safe(c).unlock("pw")
        self.assertFalse(c.locked)

    def test_execute_always_runs(self):
        for client in (FakeClient(running=False), FakeClient(locked=False), FakeClient()):
            ran = []
            safe(client).silent().unlock().execute(lambda: ran.append(1))
            self.assertEqual(ran, [1])

    def test_server_not_running_is_a_no_op(self):
        c = FakeClient(running=False)
        safe(c).unlock("pw").autolock()
        self.assertEqual(c.calls, [])

    def test_already_unlocked_makes_no_unlock_request(self):
        c = FakeClient(locked=False)
        safe(c).unlock("pw")
        self.assertEqual(c.calls, [])

    def test_wrong_or_missing_password_leaves_it_locked_and_never_autolocks_blindly(self):
        c = FakeClient()
        safe(c).silent().unlock("nope").autolock()
        self.assertTrue(c.locked)
        self.assertNotIn(("autolock",), c.calls, "autolock is only sent after a successful unlock")
        c2 = FakeClient()
        safe(c2).silent().unlock()
        self.assertTrue(c2.locked)
        self.assertEqual(c2.calls, [])

    def test_context_manager_relocks_even_on_error(self):
        os.environ["LOOM_SESSION_PASSWORD"] = "pw"
        c = FakeClient()
        with self.assertRaises(RuntimeError):
            with safe(c).unlock():
                self.assertFalse(c.locked)
                raise RuntimeError("startup failed")
        self.assertTrue(c.locked)

    def test_silent_suppresses_warnings(self):
        with self.assertLogs("bloompy.safe", level="WARNING") as loud:
            safe(FakeClient()).unlock()  # locked, no password
        self.assertTrue(loud.output)
        logger = logging.getLogger("bloompy.safe")
        with self.assertNoLogs(logger, level="WARNING"):
            safe(FakeClient()).silent().unlock()


if __name__ == "__main__":
    unittest.main()
