package config

import (
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func productionConfig() Config {
	return Config{
		ClientAttestationEnabled: true, AttestationMasterKey: make([]byte, 32), DeviceCatalog: DeviceCatalogConfig{BaseURL: "https://git.example", Repository: "owner/catalog"},
		Version:          "1.0.0",
		Addr:             ":8080",
		PublicURL:        "https://oronbox.example",
		DatabaseURL:      "postgres://localhost/oronbox",
		SessionSecret:    strings.Repeat("s", 32),
		EncryptionKey:    strings.Repeat("k", 32),
		WebClientOrigins: []string{"https://oronbox.example"},
		BandBBS:          BandBBSConfig{ClientID: "id", ClientSecret: "secret", RedirectURI: "https://oronbox.example/cb"},
		Storage:          StorageConfig{LocalRoot: "/var/lib/oronbox"},
		Limits: LimitsConfig{
			UploadMaxBytes: 1 << 20, PreviewMaxBytes: 1 << 20, PreviewMaxCount: 4,
			AuthRatePerMin: 120, AuthFailureBurst: 20, AuthFailureWindow: 15 * time.Minute,
		},
	}
}

func TestProductionConfigIsAccepted(t *testing.T) {
	t.Parallel()
	if err := productionConfig().Validate(); err != nil {
		t.Fatalf("baseline production config was rejected: %v", err)
	}
}

// A trusted proxy range decides whether a forwarding header may override the
// peer address, which is what rate limits and audit records are keyed on.
func TestOverlyBroadTrustedProxyRangeIsRejectedInProduction(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0.0.0.0/0", "0.0.0.0/4", "::/0", "2000::/16"} {
		cfg := productionConfig()
		cfg.TrustedProxyCIDRs = []string{value}
		if err := cfg.Validate(); err == nil {
			t.Errorf("TRUSTED_PROXY_CIDRS %q was accepted in production", value)
		}
	}
	for _, value := range []string{"10.0.0.0/8", "172.16.0.0/12", "127.0.0.1/32", "fd00::/48"} {
		cfg := productionConfig()
		cfg.TrustedProxyCIDRs = []string{value}
		if err := cfg.Validate(); err != nil {
			t.Errorf("TRUSTED_PROXY_CIDRS %q was rejected: %v", value, err)
		}
	}
}

// Development runs on plain HTTP behind whatever tunnel the developer has, so
// the range check must not block it.
func TestBroadTrustedProxyRangeIsToleratedInDevelopment(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()
	cfg.Version = "dev"
	cfg.TrustedProxyCIDRs = []string{"0.0.0.0/0"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("development config was rejected: %v", err)
	}
}

func TestInvalidTrustedProxyRangeIsRejected(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()
	cfg.TrustedProxyCIDRs = []string{"10.0.0.1"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("a bare address was accepted as a CIDR range")
	}
}

func TestAuthenticationLimitsMustBePositive(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*LimitsConfig){
		"rate":   func(l *LimitsConfig) { l.AuthRatePerMin = 0 },
		"burst":  func(l *LimitsConfig) { l.AuthFailureBurst = 0 },
		"window": func(l *LimitsConfig) { l.AuthFailureWindow = 0 },
	} {
		cfg := productionConfig()
		mutate(&cfg.Limits)
		if err := cfg.Validate(); err == nil {
			t.Errorf("a non-positive %s limit was accepted", name)
		}
	}
}

func TestServesHTTPSFollowsPublicURL(t *testing.T) {
	t.Parallel()
	if !(Config{PublicURL: "https://oronbox.example"}).ServesHTTPS() {
		t.Error("an HTTPS public URL was not recognised")
	}
	for _, value := range []string{"http://localhost:8080", "", "://broken"} {
		if (Config{PublicURL: value}).ServesHTTPS() {
			t.Errorf("public URL %q was treated as HTTPS", value)
		}
	}
}

func TestWeakSecretsAreRejectedInProductionOnly(t *testing.T) {
	t.Parallel()
	cfg := productionConfig()
	cfg.SessionSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Error("a short session secret was accepted in production")
	}
	cfg.Version = "dev"
	if err := cfg.Validate(); err != nil {
		t.Errorf("a short session secret blocked development startup: %v", err)
	}
}

func TestLoadIgnoresRetiredEnvironmentSettings(t *testing.T) {
	for _, key := range []string{
		"ASTROBOX_REPO_OWNER", "ASTROBOX_REPO_NAME", "ASTROBOX_REPO_BRANCH", "ASTROBOX_CATALOG_PATH",
		"GITEA_CATALOG_REPO", "GITEA_CATALOG_REF", "GITEA_CONSOLE_REPO",
		"BANDBBS_API_URL", "BANDBBS_SCOPES", "BANDBBS_PUBLISH_SCOPES", "GITHUB_API_URL", "GITHUB_SCOPES",
		"APP_VERSION", "GIT_COMMIT", "CLIENT_ATTESTATION_ENABLED", "RANKING_FRESHNESS_AMPLITUDE",
		"CLIENT_MINIMUM_VERSION", "CLIENT_REVOKED_KEYS",
	} {
		t.Setenv(key, "obsolete")
	}
	t.Setenv("APP_VERSION", "dev")
	t.Setenv("CLIENT_ATTESTATION_ENABLED", "false")
	cfg := Load()
	if !cfg.ClientAttestationEnabled || !cfg.RunningProduction() || cfg.Version != buildVersion {
		t.Fatal("retired environment settings changed production protection or build identity")
	}
	if cfg.AstroBox != (AstroBoxConfig{"AstralSightStudios", "AstroBox-Repo", "main", "index_v2.csv"}) {
		t.Fatal("retired environment settings changed the official AstroBox target")
	}
	if cfg.BandBBS.APIURL != "https://www.bandbbs.cn/api" || cfg.GitHub.APIURL != "https://api.github.com" {
		t.Fatal("retired environment settings changed official API endpoints")
	}
	if !slices.Equal(cfg.BandBBS.Scopes, ParseScopes(DefaultScopes)) || !slices.Equal(cfg.BandBBS.PublishScopes, ParseScopes(DefaultPublishScopes)) || !slices.Equal(cfg.GitHub.Scopes, ParseScopes("public_repo read:user")) {
		t.Fatal("retired environment settings changed OAuth permissions")
	}
	if cfg.ConsoleRepository != "OronBoxCommunity/OronBox-Server-Console" || cfg.DeviceCatalog.Repository != "OronBoxCommunity/OronBox-Repo" || cfg.DeviceCatalog.Ref != "main" {
		t.Fatal("retired environment settings changed configuration sources")
	}
	if cfg.MinimumClientVersion != "" || len(cfg.RevokedClientKeys) != 0 || cfg.Ranking.FreshnessAmplitude != 3 {
		t.Fatal("retired environment settings changed runtime policy defaults")
	}
}

func TestOverlyBroadProxyRangeThresholds(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"0.0.0.0/0":      true,
		"0.0.0.0/7":      true,
		"10.0.0.0/8":     false,
		"192.168.0.0/16": false,
		"::/0":           true,
		"fd00::/31":      true,
		"fd00::/32":      false,
	}
	for value, wantBroad := range tests {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			t.Fatalf("test fixture %q is not a CIDR: %v", value, err)
		}
		if got := overlyBroadProxyRange(network); got != wantBroad {
			t.Errorf("overlyBroadProxyRange(%q) = %t, want %t", value, got, wantBroad)
		}
	}
}
