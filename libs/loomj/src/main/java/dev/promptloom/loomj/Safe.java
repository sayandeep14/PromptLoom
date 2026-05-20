package dev.promptloom.loomj;

import java.util.logging.Logger;

/**
 * Fluent API for safely loading secrets in LoomLocker-protected applications.
 *
 * <p>Usage:
 * <pre>{@code
 * new Safe()
 *     .unlock()
 *     .execute(() -> loadEnv())
 *     .autolock();
 *
 * server.start();
 * }</pre>
 *
 * <p>If loomlocker is not running, all operations are transparent no-ops —
 * {@link #execute(Runnable)} always runs.
 *
 * <p>Password resolution order: {@link #unlock(String)} argument →
 * {@code LOOM_SESSION_PASSWORD} env var → skip (secrets stay locked).
 */
public class Safe {

    private static final Logger log = Logger.getLogger(Safe.class.getName());

    private final LockerConfig config;
    private final LoomLockerClient client;
    private boolean unlocked = false;
    private boolean silent = false;

    /** Create a Safe with config auto-detected from {@code .loom.config} or env vars. */
    public Safe() {
        this.config = LockerConfig.fromEnv();
        this.client = new LoomLockerClient(this.config);
    }

    /** Create a Safe with an explicit config. */
    public Safe(LockerConfig config) {
        this.config = config;
        this.client = new LoomLockerClient(config);
    }

    /** Suppress all log output from this Safe instance. */
    public Safe silent() {
        this.silent = true;
        return this;
    }

    // ── Fluent API ────────────────────────────────────────────────────────────

    /**
     * Unlock secrets using a password.
     * Falls back to {@code LOOM_SESSION_PASSWORD} env var if no argument supplied.
     * No-op if loomlocker is not running or secrets are already unlocked.
     */
    public Safe unlock(String... password) {
        if (!client.isRunning()) {
            return this;
        }
        if (!client.isLocked()) {
            unlocked = true;
            return this;
        }
        String pwd = (password.length > 0 && password[0] != null) ? password[0] : "";
        if (pwd.isEmpty()) {
            pwd = System.getenv("LOOM_SESSION_PASSWORD");
            if (pwd == null) pwd = "";
        }
        if (pwd.isEmpty()) {
            info("secrets are locked but no password provided — set LOOM_SESSION_PASSWORD");
            return this;
        }
        if (client.unlock(pwd)) {
            unlocked = true;
        } else {
            info("unlock failed: wrong password or server error");
        }
        return this;
    }

    /**
     * Run {@code fn}. Always called regardless of lock state — secrets loading
     * (e.g. {@code dotenv}) happens inside fn.
     */
    public Safe execute(Runnable fn) {
        fn.run();
        return this;
    }

    /**
     * Signal that startup is complete, triggering an immediate relock on the server.
     * No-op if loomlocker is not running or was never unlocked.
     */
    public Safe autolock() {
        if (unlocked && client.isRunning()) {
            if (!client.autolock()) {
                info("autolock request failed");
            }
            unlocked = false;
        }
        return this;
    }

    // ── Internal ──────────────────────────────────────────────────────────────

    private void info(String msg) {
        if (!silent) {
            log.info("[loomj] " + msg);
        }
    }
}
