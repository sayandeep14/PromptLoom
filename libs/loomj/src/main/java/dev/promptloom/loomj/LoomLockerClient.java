package dev.promptloom.loomj;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.time.Duration;
import java.util.logging.Logger;

/**
 * HTTP client for the loomlocker server.
 *
 * <p>Uses {@code java.net.http.HttpClient} (Java 11+) with no external dependencies.
 * All methods silently return false/null on network errors — if loomlocker is not
 * running, they are no-ops.
 */
public class LoomLockerClient {

    private static final Logger log = Logger.getLogger(LoomLockerClient.class.getName());

    private final LockerConfig config;
    private final HttpClient http;

    public LoomLockerClient(LockerConfig config) {
        this.config = config;
        this.http = HttpClient.newBuilder()
                .connectTimeout(Duration.ofSeconds(3))
                .build();
    }

    /** Return true if the loomlocker server is reachable. */
    public boolean isRunning() {
        try {
            HttpResponse<String> resp = get("/ping");
            return resp != null && resp.statusCode() == 200;
        } catch (Exception e) {
            return false;
        }
    }

    /** Return the current lock state. Returns false if unreachable. */
    public boolean isLocked() {
        try {
            HttpResponse<String> resp = get("/ping");
            if (resp == null) return false;
            return resp.body().contains("\"locked\":true");
        } catch (Exception e) {
            return false;
        }
    }

    /**
     * Send a password to unlock secrets.
     *
     * @return true on success, false on failure or wrong password
     */
    public boolean unlock(String password) {
        try {
            String body = "{\"password\":\"" + escapeJson(password) + "\"}";
            HttpResponse<String> resp = post("/unlock", body);
            return resp != null && resp.statusCode() == 200;
        } catch (Exception e) {
            log.fine("loomj: unlock request failed: " + e.getMessage());
            return false;
        }
    }

    /** Request an immediate lock (no password required). */
    public boolean lock() {
        try {
            HttpResponse<String> resp = post("/lock", "{}");
            return resp != null && resp.statusCode() == 200;
        } catch (Exception e) {
            return false;
        }
    }

    /** Signal that startup is complete → trigger immediate autolock on the server. */
    public boolean autolock() {
        try {
            HttpResponse<String> resp = post("/autolock", "{}");
            return resp != null && resp.statusCode() == 200;
        } catch (Exception e) {
            return false;
        }
    }

    // ── Private helpers ───────────────────────────────────────────────────────

    private HttpResponse<String> get(String path) throws Exception {
        HttpRequest req = HttpRequest.newBuilder()
                .uri(URI.create(config.baseUrl() + path))
                .timeout(Duration.ofSeconds(4))
                .GET()
                .build();
        return http.send(req, HttpResponse.BodyHandlers.ofString());
    }

    private HttpResponse<String> post(String path, String jsonBody) throws Exception {
        HttpRequest req = HttpRequest.newBuilder()
                .uri(URI.create(config.baseUrl() + path))
                .timeout(Duration.ofSeconds(5))
                .header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(jsonBody))
                .build();
        return http.send(req, HttpResponse.BodyHandlers.ofString());
    }

    private static String escapeJson(String s) {
        return s.replace("\\", "\\\\").replace("\"", "\\\"");
    }
}
