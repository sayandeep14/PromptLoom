package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"PORT", "UPLOAD_SECRET", "UPLOAD_TOKENS", "CORS_ORIGINS", "MAX_BODY_BYTES",
		"RATE_LIMIT_READ_PER_MIN", "RATE_LIMIT_WRITE_PER_MIN", "TRUST_PROXY", "AUTO_MIGRATE"} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestDefaults(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "8080" || len(c.Tokens) != 0 || len(c.CORSOrigins) != 0 || c.TrustProxy {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.MaxBodyBytes != 8<<20 || c.ReadRPM != 120 || c.WriteRPM != 10 {
		t.Errorf("unexpected limits: %+v", c)
	}
}

func TestShortSecretRejected(t *testing.T) {
	setEnv(t, map[string]string{"UPLOAD_SECRET": "short"})
	if _, err := Load(); err == nil {
		t.Fatal("a short UPLOAD_SECRET must be rejected")
	}
}

func TestParsing(t *testing.T) {
	setEnv(t, map[string]string{
		"UPLOAD_SECRET": "0123456789abcdef", "CORS_ORIGINS": " https://a.example , https://b.example ,",
		"TRUST_PROXY": "true", "MAX_BODY_BYTES": "1000",
	})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.CORSOrigins) != 2 || c.CORSOrigins[1] != "https://b.example" || !c.TrustProxy || c.MaxBodyBytes != 1000 {
		t.Errorf("parse result wrong: %+v", c)
	}
}

func TestBadNumbers(t *testing.T) {
	for _, kv := range []map[string]string{
		{"MAX_BODY_BYTES": "abc"}, {"MAX_BODY_BYTES": "0"}, {"RATE_LIMIT_WRITE_PER_MIN": "-1"},
	} {
		setEnv(t, kv)
		if _, err := Load(); err == nil {
			t.Errorf("%v should fail", kv)
		}
	}
}

func TestAutoMigrate(t *testing.T) {
	setEnv(t, nil)
	if c, _ := Load(); c.AutoMigrate {
		t.Error("AUTO_MIGRATE must default to off")
	}
	setEnv(t, map[string]string{"AUTO_MIGRATE": "1"})
	if c, _ := Load(); !c.AutoMigrate {
		t.Error("AUTO_MIGRATE=1 should enable it")
	}
}

func TestTokens(t *testing.T) {
	const a, b, c = "aaaaaaaaaaaaaaaa1", "bbbbbbbbbbbbbbbb2", "cccccccccccccccc3"
	setEnv(t, map[string]string{
		"UPLOAD_SECRET": a + " , " + b, // two admin secrets at once: rotation
		"UPLOAD_TOKENS": "alice=" + c + ", bob.dev=" + c + "x ,alice=" + c + "y",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tk := range cfg.Tokens {
		got = append(got, tk.Name+":"+strings.TrimLeft(tk.Secret, "abc")+":"+map[bool]string{true: "admin", false: "pub"}[tk.Admin])
	}
	want := "admin:1:admin admin:2:admin alice:3:pub bob.dev:3x:pub alice:3y:pub"
	if strings.Join(got, " ") != want {
		t.Errorf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
}

func TestBadTokensAreRejectedWithoutEchoingSecrets(t *testing.T) {
	const good = "0123456789abcdefXYZ"
	for name, kv := range map[string]map[string]string{
		"short admin secret":  {"UPLOAD_SECRET": "short"},
		"short publisher":     {"UPLOAD_TOKENS": "alice=short"},
		"missing =":           {"UPLOAD_TOKENS": good},
		"bad name":            {"UPLOAD_TOKENS": "Alice Smith=" + good},
		"empty name":          {"UPLOAD_TOKENS": "=" + good},
		"reserved admin":      {"UPLOAD_TOKENS": "admin=" + good},
		"secret shared":       {"UPLOAD_TOKENS": "alice=" + good + ",bob=" + good},
		"admin secret reused": {"UPLOAD_SECRET": good, "UPLOAD_TOKENS": "alice=" + good},
	} {
		setEnv(t, kv)
		_, err := Load()
		if err == nil {
			t.Errorf("%s: must be rejected", name)
			continue
		}
		if strings.Contains(err.Error(), good) {
			t.Errorf("%s: the error echoes a secret: %v", name, err)
		}
	}
}
