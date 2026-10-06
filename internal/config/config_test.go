package config

import (
	"strings"
	"testing"
)

func TestValidateRequiresSecurityConfiguration(t *testing.T) {
	c := Config{Addr: ":6767", ClientAttestationEnabled: false}
	if e := c.Validate(); e == nil {
		t.Fatal("incomplete configuration accepted")
	}
	c.DatabaseURL = "postgres://x"
	c.SessionSecret = strings.Repeat("s", 32)
	c.TokenEncryptionKey = strings.Repeat("k", 32)
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.ClientAttestationEnabled = true
	if e := c.Validate(); e == nil {
		t.Fatal("missing attestation master key accepted")
	}
	c.AttestationMasterKey = make([]byte, 32)
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
}

func TestDecodeMasterKey(t *testing.T) {
	if len(decodeHexKey(strings.Repeat("ab", 32))) != 32 {
		t.Fatal("valid 32-byte hex master key rejected")
	}
	if decodeHexKey("not-hex") != nil {
		t.Fatal("invalid hex master key accepted")
	}
	if decodeHexKey(strings.Repeat("ab", 16)) != nil {
		t.Fatal("short master key accepted")
	}
}
