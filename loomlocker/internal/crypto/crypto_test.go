package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(h), "correct horse") {
		t.Error("the hash must not contain the password")
	}
	if !VerifyPassword("correct horse", h) {
		t.Error("the right password must verify")
	}
	for _, bad := range []string{"", "correct horse ", "Correct horse", "wrong"} {
		if VerifyPassword(bad, h) {
			t.Errorf("%q must not verify", bad)
		}
	}
	h2, _ := HashPassword("correct horse")
	if bytes.Equal(h, h2) {
		t.Error("hashes are salted: two hashes of the same password must differ")
	}
	if _, err := HashPassword(strings.Repeat("x", 100)); err == nil {
		t.Error("bcrypt silently ignores bytes past 72: a longer password must be rejected, not truncated")
	}
}

func TestRandomKeyAndTokens(t *testing.T) {
	a, _ := RandomKey()
	b, _ := RandomKey()
	if len(a) != 32 || bytes.Equal(a, b) {
		t.Errorf("keys must be 32 random bytes: %d, equal=%v", len(a), bytes.Equal(a, b))
	}
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		tok := RandomToken()
		if !IsToken(tok) {
			t.Fatalf("RandomToken produced %q which IsToken rejects", tok)
		}
		if seen[tok] {
			t.Fatalf("token repeated after %d draws", i)
		}
		seen[tok] = true
	}
}

func TestIsToken(t *testing.T) {
	yes := []string{"lk_0123456789abcdef", "lk_ffffffffffffffff"}
	no := []string{"", "lk_", "lk_0123456789abcde", "lk_0123456789abcdefg", "LK_0123456789abcdef",
		"lk_0123456789abcdeg", "xx_0123456789abcdef", "real-secret-value", " lk_0123456789abcdef"}
	for _, s := range yes {
		if !IsToken(s) {
			t.Errorf("%q should be a token", s)
		}
	}
	for _, s := range no {
		if IsToken(s) {
			t.Errorf("%q should not be a token", s)
		}
	}
}

func TestDeriveKey(t *testing.T) {
	k1, salt, err := DeriveKey("pw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 32 || len(salt) != saltLen {
		t.Fatalf("key %d bytes, salt %d bytes", len(k1), len(salt))
	}
	k2, _, _ := DeriveKey("pw", salt)
	if !bytes.Equal(k1, k2) {
		t.Error("the same password and salt must give the same key (recovery depends on it)")
	}
	k3, _, _ := DeriveKey("other", salt)
	if bytes.Equal(k1, k3) {
		t.Error("a different password must give a different key")
	}
	k4, salt2, _ := DeriveKey("pw", nil)
	if bytes.Equal(salt, salt2) || bytes.Equal(k1, k4) {
		t.Error("each fresh derivation needs a new random salt")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, _ := RandomKey()
	plain := []byte(`{"file":{"lk_x":"secret"}}`)

	ct, err := Encrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("secret")) {
		t.Error("ciphertext must not contain the plaintext")
	}
	got, err := Decrypt(ct, key)
	if err != nil || !bytes.Equal(got, plain) {
		t.Errorf("round trip: %q %v", got, err)
	}

	ct2, _ := Encrypt(plain, key)
	if bytes.Equal(ct, ct2) {
		t.Error("a fresh nonce per message: identical plaintexts must encrypt differently")
	}

	other, _ := RandomKey()
	if _, err := Decrypt(ct, other); err == nil {
		t.Error("the wrong key must fail")
	}
	// GCM authenticates: flipping any single byte must be detected
	for i := range ct {
		tampered := append([]byte(nil), ct...)
		tampered[i] ^= 0x01
		if _, err := Decrypt(tampered, key); err == nil {
			t.Fatalf("tampering with byte %d went undetected", i)
		}
	}
	if _, err := Decrypt(ct[:5], key); err == nil {
		t.Error("truncated ciphertext must fail")
	}
	if _, err := Encrypt(plain, []byte("short")); err == nil {
		t.Error("an invalid key size must be an error")
	}
}
