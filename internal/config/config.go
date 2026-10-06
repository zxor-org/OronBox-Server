package config

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr, PublicURL, DatabaseURL, SessionSecret, TokenEncryptionKey string
	GitHubReleaseToken                                              string
	GiteaPublicURL                                                  string
	AttestationMasterKey                                            []byte
	ClientAttestationEnabled                                        bool
	ClientAttestationSkew                                           time.Duration
	ConsoleDir                                                      string
	BandBBS, Gitea, GitHub                                          EndpointConfig
	AstroBox                                                        AstroBoxConfig
	AdminBandBBSUIDs                                                []string
	ClientRedirectURI                                               string
	WebClientOrigins                                                []string
	StateTTL, LoginTicketTTL, AccessTokenTTL, RefreshTokenTTL       time.Duration
	LogLevel, LogFormat                                             string
}
type EndpointConfig struct {
	ClientID, ClientSecret, RedirectURI, AuthorizeURL, TokenURL, APIURL, DeviceCodeURL, MeURL, Scopes, CatalogRepo, ConsoleRepo string
	IntrospectURL, RevokeURL                                                                                       string
	PublishScopes                                                                                                  []string
}

// AstroBoxConfig is the downstream AstroBox catalog repo (staging submission target).
type AstroBoxConfig struct{ RepoOwner, RepoName, RepoBranch, CatalogPath string }

func Load() Config {
	loadDotEnv(".env")
	return Config{
		Addr: env("ADDR", ":6767"), PublicURL: strings.TrimRight(env("PUBLIC_URL", "http://localhost:6767"), "/"), DatabaseURL: env("DATABASE_URL", ""),
		SessionSecret: env("SESSION_SECRET", ""), TokenEncryptionKey: env("TOKEN_ENCRYPTION_KEY", ""), GitHubReleaseToken: env("GITHUB_TOKEN", ""), GiteaPublicURL: strings.TrimRight(env("GITEA_URL", "https://git.zxor.org"), "/"),
		AttestationMasterKey: decodeHexKey(env("OB_AUTH_MASTER", "")), ClientAttestationEnabled: boolEnv("CLIENT_ATTESTATION_ENABLED", true), ClientAttestationSkew: durationEnv("CLIENT_ATTESTATION_SKEW_SEC", 300*time.Second),
		ConsoleDir: env("CONSOLE_DIR", "console"),
		BandBBS: EndpointConfig{ClientID: env("BANDBBS_CLIENT_ID", ""), ClientSecret: env("BANDBBS_CLIENT_SECRET", ""), RedirectURI: env("BANDBBS_REDIRECT_URI", ""), AuthorizeURL: env("BANDBBS_AUTHORIZE_URL", "https://www.bandbbs.cn/oauth2/authorize"), TokenURL: env("BANDBBS_TOKEN_URL", "https://www.bandbbs.cn/api/oauth2/token"), APIURL: env("BANDBBS_API_URL", "https://www.bandbbs.cn/api"), MeURL: env("BANDBBS_ME_URL", "https://www.bandbbs.cn/api/me"), IntrospectURL: env("BANDBBS_INTROSPECT_URL", "https://www.bandbbs.cn/api/oauth2/introspect"), RevokeURL: env("BANDBBS_REVOKE_URL", "https://www.bandbbs.cn/api/oauth2/revoke"), Scopes: env("BANDBBS_SCOPES", ""), PublishScopes: fieldsEnv("BANDBBS_PUBLISH_SCOPES", "resource:read resource_category:read resource_check:read resource_rating:read thread:read attachment:read resource:write thread:write attachment:write")},
		Gitea:   EndpointConfig{APIURL: env("GITEA_INTRANET_URL", env("GITEA_URL", "https://git.zxor.org")), ClientSecret: env("GITEA_BOT_TOKEN", ""), CatalogRepo: env("GITEA_CATALOG_REPO", "OronBoxCommunity/OronBox-Repo"), ConsoleRepo: env("GITEA_CONSOLE_REPO", "OronBoxCommunity/OronBox-Server-Console")}, GitHub: EndpointConfig{ClientID: env("GITHUB_CLIENT_ID", ""), ClientSecret: env("GITHUB_CLIENT_SECRET", ""), RedirectURI: env("GITHUB_REDIRECT_URI", ""), AuthorizeURL: env("GITHUB_AUTHORIZE_URL", "https://github.com/login/oauth/authorize"), TokenURL: env("GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"), APIURL: env("GITHUB_API_URL", "https://api.github.com"), DeviceCodeURL: env("GITHUB_DEVICE_CODE_URL", "https://github.com/login/device/code"), Scopes: env("GITHUB_SCOPES", "repo read:user")},
		AstroBox:         AstroBoxConfig{RepoOwner: env("ASTROBOX_REPO_OWNER", "AstralSightStudios"), RepoName: env("ASTROBOX_REPO_NAME", "AstroBox-Repo"), RepoBranch: env("ASTROBOX_REPO_BRANCH", "main"), CatalogPath: env("ASTROBOX_CATALOG_PATH", "index_v2.csv")},
		AdminBandBBSUIDs: listEnv("ADMIN_BANDBBS_UIDS", "ADMIN_BANDBBS_USER_IDS"), ClientRedirectURI: env("CLIENT_REDIRECT_URI", "oronbox://oauth/bandbbs"), WebClientOrigins: listEnv("WEB_CLIENT_ORIGINS"),
		StateTTL: durationEnv("STATE_TTL", 10*time.Minute), LoginTicketTTL: durationEnv("LOGIN_TICKET_TTL", 3*time.Minute), AccessTokenTTL: durationEnv("ACCESS_TOKEN_TTL", 15*time.Minute), RefreshTokenTTL: durationEnv("REFRESH_TOKEN_TTL", 720*time.Hour), LogLevel: env("LOG_LEVEL", "info"), LogFormat: env("LOG_FORMAT", "text"),
	}
}

func (c Config) Validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(c.SessionSecret) < 32 {
		missing = append(missing, "SESSION_SECRET (at least 32 bytes)")
	}
	if len(c.TokenEncryptionKey) < 32 {
		missing = append(missing, "TOKEN_ENCRYPTION_KEY (at least 32 bytes)")
	}
	if c.Addr == "" {
		missing = append(missing, "ADDR")
	}
	if c.ClientAttestationEnabled && len(c.AttestationMasterKey) != 32 {
		missing = append(missing, "OB_AUTH_MASTER (32-byte hex)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

func decodeHexKey(value string) []byte {
	b, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}
func env(k, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return fallback
}
func fieldsEnv(k, fallback string) []string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		v = fallback
	}
	return strings.Fields(v)
}
func listEnv(keys ...string) []string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			var out []string
			for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
				if p != "" {
					out = append(out, p)
				}
			}
			return out
		}
	}
	return nil
}
func boolEnv(k string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return fallback
	}
	b, e := strconv.ParseBool(v)
	if e != nil {
		return fallback
	}
	return b
}
func durationEnv(k string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return fallback
	}
	if n, e := strconv.Atoi(v); e == nil {
		return time.Duration(n) * time.Second
	}
	if d, e := time.ParseDuration(v); e == nil && d > 0 {
		return d
	}
	return fallback
}
func loadDotEnv(path string) {
	f, e := os.Open(path)
	if e != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if ok && os.Getenv(strings.TrimSpace(key)) == "" {
			os.Setenv(strings.TrimSpace(key), strings.Trim(strings.TrimSpace(val), "\"'"))
		}
	}
}
