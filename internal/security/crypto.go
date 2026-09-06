package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Cipher struct{ aead cipher.AEAD }
type envelope struct {
	Version int    `json:"v"`
	Nonce   string `json:"nonce"`
	Data    string `json:"ciphertext"`
}

func LoadOrCreateCipher(path string) (*Cipher, bool, error) {
	b, err := os.ReadFile(path)
	created := false
	if os.IsNotExist(err) {
		b = make([]byte, 32)
		if _, err = rand.Read(b); err != nil {
			return nil, false, err
		}
		if err = os.WriteFile(path, []byte(base64.RawStdEncoding.EncodeToString(b)), 0600); err != nil {
			return nil, false, err
		}
		created = true
	} else if err != nil {
		return nil, false, err
	} else {
		b, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, false, fmt.Errorf("decode master key: %w", err)
		}
	}
	if len(b) != 32 {
		return nil, false, fmt.Errorf("master key must contain 32 bytes")
	}
	block, err := aes.NewCipher(b)
	if err != nil {
		return nil, false, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, false, err
	}
	return &Cipher{aead: aead}, created, nil
}

func (c *Cipher) Encrypt(plain []byte) (string, error) {
	n := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return "", err
	}
	e := envelope{Version: 1, Nonce: base64.RawStdEncoding.EncodeToString(n), Data: base64.RawStdEncoding.EncodeToString(c.aead.Seal(nil, n, plain, []byte("webssh:v1")))}
	b, err := json.Marshal(e)
	return string(b), err
}

func (c *Cipher) Decrypt(value string) ([]byte, error) {
	var e envelope
	if err := json.Unmarshal([]byte(value), &e); err != nil {
		return nil, err
	}
	if e.Version != 1 {
		return nil, fmt.Errorf("unsupported key version")
	}
	n, err := base64.RawStdEncoding.DecodeString(e.Nonce)
	if err != nil {
		return nil, err
	}
	b, err := base64.RawStdEncoding.DecodeString(e.Data)
	if err != nil {
		return nil, err
	}
	return c.aead.Open(nil, n, b, []byte("webssh:v1"))
}
