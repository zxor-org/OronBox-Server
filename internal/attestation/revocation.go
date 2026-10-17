package attestation

import "strings"

// Revocation is the per-version key-fingerprint blacklist. The global minimum
// version is carried separately in Options.MinimumVersion.
type Revocation struct {
	Fingerprints map[string]struct{}
}

func NewRevocation(fingerprints []string) Revocation {
	r := Revocation{Fingerprints: map[string]struct{}{}}
	for _, f := range fingerprints {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			r.Fingerprints[f] = struct{}{}
		}
	}
	return r
}

func (r Revocation) IsRevoked(fingerprint string) bool {
	if len(r.Fingerprints) == 0 {
		return false
	}
	_, ok := r.Fingerprints[strings.ToLower(strings.TrimSpace(fingerprint))]
	return ok
}
