package dev.promptloom.loomj;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Holds connection settings for the loomlocker server.
 * Auto-detected from {@code .loom.config} or environment variables.
 */
public class LockerConfig {

    private String host;
    private String port;

    public LockerConfig(String host, String port) {
        this.host = host;
        this.port = port;
    }

    public String getHost() { return host; }
    public String getPort() { return port; }

    /** Full API base URL, e.g. {@code http://localhost:8053/api}. */
    public String baseUrl() {
        return host + ":" + port + "/api";
    }

    /**
     * Build config from env vars, falling back to {@code .loom.config} file,
     * then to built-in defaults.
     */
    public static LockerConfig fromEnv() {
        LockerConfig cfg = fromFile();
        if (cfg == null) {
            cfg = new LockerConfig("http://localhost", "8053");
        }
        String envHost = System.getenv("LOOM_HOST");
        String envPort = System.getenv("LOOM_PORT");
        if (envHost != null && !envHost.isEmpty()) cfg.host = envHost;
        if (envPort != null && !envPort.isEmpty()) cfg.port = envPort;
        return cfg;
    }

    private static LockerConfig fromFile() {
        Path configPath = findLoomConfig();
        if (configPath == null) return null;
        try {
            String json = Files.readString(configPath);
            // Minimal JSON field extraction without an external dependency.
            String host = extractJsonString(json, "lockhost");
            String port = extractJsonString(json, "port");
            if (host == null) host = "http://localhost";
            if (port == null) port = "8053";
            return new LockerConfig(host, port);
        } catch (IOException e) {
            return null;
        }
    }

    private static Path findLoomConfig() {
        Path dir = Paths.get(System.getProperty("user.dir")).toAbsolutePath();
        while (dir != null) {
            Path candidate = dir.resolve(".loom.config");
            if (Files.exists(candidate)) return candidate;
            Path parent = dir.getParent();
            if (parent == null || parent.equals(dir)) break;
            dir = parent;
        }
        return null;
    }

    private static String extractJsonString(String json, String key) {
        Matcher m = Pattern.compile("\"" + Pattern.quote(key) + "\"\\s*:\\s*\"([^\"]+)\"")
                .matcher(json);
        return m.find() ? m.group(1) : null;
    }
}
