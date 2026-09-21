package config

import "testing"

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"PORT", "UPLOAD_SECRET", "CORS_ORIGINS", "MAX_BODY_BYTES",
		"RATE_LIMIT_READ_PER_MIN", "RATE_LIMIT_WRITE_PER_MIN", "TRUST_PROXY"} {
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
	if c.Port != "8080" || c.UploadSecret != "" || len(c.CORSOrigins) != 0 || c.TrustProxy {
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
