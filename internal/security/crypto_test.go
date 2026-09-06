package security

import (
	"strings"
	"testing"
)

func TestCipherRoundTripAndTamper(t *testing.T) {
	c, created, err := LoadOrCreateCipher(t.TempDir() + "/master.key")
	if err != nil || !created {
		t.Fatalf("create cipher: created=%v err=%v", created, err)
	}
	plain := []byte("a-password-that-must-not-be-stored")
	value, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value, string(plain)) {
		t.Fatal("ciphertext leaked plaintext")
	}
	got, err := c.Decrypt(value)
	if err != nil || string(got) != string(plain) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	value = strings.Replace(value, "ciphertext\":\"", "ciphertext\":\"A", 1)
	if _, err = c.Decrypt(value); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
