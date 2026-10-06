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

type Cipher struct{ aead cipher.AEAD }

func NewCipher(secret string) (*Cipher, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("encryption key must be at least 32 bytes")
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}
func (c *Cipher) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, fmt.Errorf("ciphertext is truncated")
	}
	return c.aead.Open(nil, ciphertext[:c.aead.NonceSize()], ciphertext[c.aead.NonceSize():], nil)
}
func (c *Cipher) EncryptString(value string) (string, error) {
	b, e := c.Encrypt([]byte(value))
	if e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}
func (c *Cipher) DecryptString(value string) (string, error) {
	b, e := base64.RawStdEncoding.DecodeString(value)
	if e != nil {
		return "", e
	}
	p, e := c.Decrypt(b)
	return string(p), e
}
func HashToken(value string) []byte { s := sha256.Sum256([]byte(value)); return s[:] }
