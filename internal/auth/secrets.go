package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

type Secrets struct{ aead cipher.AEAD }

func NewSecrets(key string) (*Secrets, error) {
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Secrets{aead: aead}, nil
}

func (s *Secrets) Encrypt(plain string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (s *Secrets) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) < s.aead.NonceSize() {
		return "", fmt.Errorf("encrypted value is truncated")
	}
	nonce, payload := ciphertext[:s.aead.NonceSize()], ciphertext[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, payload, nil)
	return string(plain), err
}

func RandomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func HashToken(value, pepper string) []byte {
	sum := sha256.Sum256([]byte(pepper + "\x00" + value))
	return sum[:]
}
