package auth

import "testing"

func TestCipherRoundTripAndTamper(t *testing.T) {
	c, e := NewCipher("01234567890123456789012345678901")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := c.Encrypt([]byte("secret"))
	if e != nil {
		t.Fatal(e)
	}
	plain, e := c.Decrypt(raw)
	if e != nil || string(plain) != "secret" {
		t.Fatalf("round trip: %q %v", plain, e)
	}
	raw[len(raw)-1] ^= 1
	if _, e := c.Decrypt(raw); e == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
func TestCipherRejectsWeakAndTruncatedKeys(t *testing.T) {
	if _, e := NewCipher("short"); e == nil {
		t.Fatal("weak key accepted")
	}
	c, _ := NewCipher("01234567890123456789012345678901")
	if _, e := c.Decrypt([]byte("x")); e == nil {
		t.Fatal("truncated ciphertext accepted")
	}
	if _, e := c.DecryptString("%%%bad"); e == nil {
		t.Fatal("invalid base64 accepted")
	}
}
func TestStringEncodingAndHash(t *testing.T) {
	c, _ := NewCipher("01234567890123456789012345678901")
	encoded, e := c.EncryptString("hello")
	if e != nil {
		t.Fatal(e)
	}
	plain, e := c.DecryptString(encoded)
	if e != nil || plain != "hello" {
		t.Fatalf("%q %v", plain, e)
	}
	if len(HashToken("hello")) != 32 {
		t.Fatal("unexpected token hash length")
	}
}
