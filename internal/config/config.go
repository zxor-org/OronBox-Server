package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const DefaultScopes = "user:read resource_check:read resource:read resource_category:read resource_rating:read thread:read"
const DefaultPublishScopes = "attachment:write resource:write"

type Config struct {
	Addr              string
	PublicURL         string
	DatabaseURL       string
	SessionSecret     string
	EncryptionKey     string
	BandBBS           BandBBSConfig
	GitHub            GitHubConfig
	AstroBox          AstroBoxConfig
	Storage           StorageConfig
	Limits            LimitsConfig
	ClientRedirectURI string
	WebClientOrigins  []string
	TrustedProxyCIDRs []string
	StateTTL          time.Duration
	LoginTicketTTL    time.Duration
	AccessTokenTTL    time.Duration
	RefreshTokenTTL   time.Duration
	Admin             AdminConfig
	Moderation        ModerationConfig
	Ranking           RankingConfig
	Retention         RetentionConfig
	Version           string
	Commit            string
	LogLevel          string
	LogFormat         string
}

type BandBBSConfig struct {
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	AuthorizeURL  string
	TokenURL      string
	MeURL         string
	APIURL        string
	IntrospectURL string
	RevokeURL     string
	Scopes        []string
	PublishScopes []string
}

type GitHubConfig struct {
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	AuthorizeURL  string
	DeviceCodeURL string
	TokenURL      string
	APIURL        string
	Scopes        []string
}

type AstroBoxConfig struct {
	RepoOwner   string
	RepoName    string
	RepoBranch  string
	CatalogPath string
}

type StorageConfig struct {
	LocalRoot string
	R2        R2Config
}

type R2Config struct {
	Enabled         bool
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PublicBaseURL   string
}

type LimitsConfig struct {
	UploadMaxBytes     int64
	PreviewMaxBytes    int64
	PreviewMaxCount    int
	DownloadRatePerMin int
	DownloadDailyLimit int
	DownloadPresignTTL time.Duration
	// Credential endpoints (ticket exchange and session refresh) are capped
	// per client IP. The attempt ceiling stays generous so shared NAT egress
	// keeps working, while the failure budget is what actually stops online
	// guessing.
	AuthRatePerMin    int
	AuthFailureBurst  int
	AuthFailureWindow time.Duration
}

type AdminConfig struct {
	BandBBSUserIDs []int64
}

// RankingConfig tunes the multipliers of the recommendation score. Values are
// formatted into SQL literals, so each is clamped to a positive finite range
// on load.
type RankingConfig struct {
	// CoinExtraWeight scales coin balance beyond the coiner count inside the
	// ln() engagement term.
	CoinExtraWeight float64
	// DownloadWeight scales the download count inside the ln() term.
	DownloadWeight float64
	// FreshnessAmplitude is the peak boost of a brand-new item; the boost is
	// FreshnessAmplitude * exp(-age_days/FreshnessDecayDays).
	FreshnessAmplitude float64
	// FreshnessDecayDays is the e-folding window of the freshness boost.
	FreshnessDecayDays float64
	// FeaturedBoost multiplies the score of featured items.
	FeaturedBoost float64
	// JitterBase is the deterministic shuffle offset; the effective jitter
	// ranges from JitterBase to JitterBase+1 per request seed.
	JitterBase float64
}

type ModerationEndpointConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

type ModerationConfig struct {
	Primary  ModerationEndpointConfig
	Fallback ModerationEndpointConfig
	Timeout  time.Duration
}

type RetentionConfig struct{ Unpublished, Audit, Feedback, OrphanBlobs, Interval time.Duration }

func Load() Config {
	return Config{
		Addr:          env("ADDR", ":8080"),
		PublicURL:     strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:   env("DATABASE_URL", "postgres://oronbox:oronbox@127.0.0.1:5432/oronbox?sslmode=disable"),
		SessionSecret: env("SESSION_SECRET", ""),
		EncryptionKey: env("TOKEN_ENCRYPTION_KEY", ""),
		BandBBS: BandBBSConfig{
			ClientID:      env("BANDBBS_CLIENT_ID", ""),
			ClientSecret:  env("BANDBBS_CLIENT_SECRET", ""),
			RedirectURI:   env("BANDBBS_REDIRECT_URI", "http://localhost:8080/oauth2/bandbbs/callback"),
			AuthorizeURL:  env("BANDBBS_AUTHORIZE_URL", "https://www.bandbbs.cn/oauth2/authorize"),
			TokenURL:      env("BANDBBS_TOKEN_URL", "https://www.bandbbs.cn/api/oauth2/token"),
			MeURL:         env("BANDBBS_ME_URL", "https://www.bandbbs.cn/api/me"),
			APIURL:        strings.TrimRight(env("BANDBBS_API_URL", "https://www.bandbbs.cn/api"), "/"),
			IntrospectURL: env("BANDBBS_INTROSPECT_URL", "https://www.bandbbs.cn/api/oauth2/introspect"),
			RevokeURL:     env("BANDBBS_REVOKE_URL", "https://www.bandbbs.cn/api/oauth2/revoke"),
			Scopes:        ParseScopes(env("BANDBBS_SCOPES", DefaultScopes)),
			PublishScopes: ParseScopes(env("BANDBBS_PUBLISH_SCOPES", DefaultPublishScopes)),
		},
		GitHub: GitHubConfig{
			ClientID:      env("GITHUB_CLIENT_ID", ""),
			ClientSecret:  env("GITHUB_CLIENT_SECRET", ""),
			RedirectURI:   env("GITHUB_REDIRECT_URI", ""),
			AuthorizeURL:  env("GITHUB_AUTHORIZE_URL", "https://github.com/login/oauth/authorize"),
			DeviceCodeURL: env("GITHUB_DEVICE_CODE_URL", "https://github.com/login/device/code"),
			TokenURL:      env("GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"),
			APIURL:        strings.TrimRight(env("GITHUB_API_URL", "https://api.github.com"), "/"),
			Scopes:        ParseScopes(env("GITHUB_SCOPES", "public_repo read:user")),
		},
		AstroBox: AstroBoxConfig{
			RepoOwner:   env("ASTROBOX_REPO_OWNER", "AstralSightStudios"),
			RepoName:    env("ASTROBOX_REPO_NAME", "AstroBox-Repo"),
			RepoBranch:  env("ASTROBOX_REPO_BRANCH", "main"),
			CatalogPath: env("ASTROBOX_CATALOG_PATH", "index_v2.csv"),
		},
		Storage: StorageConfig{
			LocalRoot: env("STORAGE_LOCAL_ROOT", "./data/blobs"),
			R2: R2Config{
				Enabled:         boolEnv("R2_ENABLED", false),
				Endpoint:        env("R2_ENDPOINT", ""),
				Region:          env("R2_REGION", "auto"),
				Bucket:          env("R2_BUCKET", ""),
				AccessKeyID:     env("R2_ACCESS_KEY_ID", ""),
				SecretAccessKey: env("R2_SECRET_ACCESS_KEY", ""),
				PublicBaseURL:   strings.TrimRight(env("R2_PUBLIC_BASE_URL", ""), "/"),
			},
		},
		Limits: LimitsConfig{
			UploadMaxBytes:     int64Env("UPLOAD_MAX_BYTES", 100<<20),
			PreviewMaxBytes:    int64Env("PREVIEW_MAX_BYTES", 10<<20),
			PreviewMaxCount:    int(int64Env("PREVIEW_MAX_COUNT", 12)),
			DownloadRatePerMin: int(int64Env("DOWNLOAD_RATE_PER_MINUTE", 30)),
			DownloadDailyLimit: int(int64Env("DOWNLOAD_DAILY_LIMIT", 200)),
			DownloadPresignTTL: durationEnv("DOWNLOAD_PRESIGN_TTL", 10*time.Minute),
			AuthRatePerMin:     int(int64Env("AUTH_RATE_PER_MINUTE", 120)),
			AuthFailureBurst:   int(int64Env("AUTH_FAILURE_BURST", 20)),
			AuthFailureWindow:  durationEnv("AUTH_FAILURE_WINDOW", 15*time.Minute),
		},
		ClientRedirectURI: env("CLIENT_REDIRECT_URI", "oronbox://oauth/bandbbs"),
		WebClientOrigins:  stringListEnv("WEB_CLIENT_ORIGINS"),
		TrustedProxyCIDRs: stringListEnv("TRUSTED_PROXY_CIDRS"),
		StateTTL:          durationEnv("STATE_TTL", 10*time.Minute),
		LoginTicketTTL:    durationEnv("LOGIN_TICKET_TTL", 3*time.Minute),
		AccessTokenTTL:    durationEnv("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:   durationEnv("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		Admin: AdminConfig{
			BandBBSUserIDs: int64ListEnv("ADMIN_BANDBBS_USER_IDS"),
		},
		Moderation: ModerationConfig{
			Primary: ModerationEndpointConfig{
				BaseURL: strings.TrimRight(env("MODERATION_BASE_URL", "https://api.deepseek.com"), "/"),
				APIKey:  env("MODERATION_API_KEY", ""),
				Model:   env("MODERATION_MODEL", "deepseek-v4-flash"),
			},
			Fallback: ModerationEndpointConfig{
				BaseURL: strings.TrimRight(env("MODERATION_FALLBACK_BASE_URL", "https://open.bigmodel.cn/api/paas/v4"), "/"),
				APIKey:  env("MODERATION_FALLBACK_API_KEY", ""),
				Model:   env("MODERATION_FALLBACK_MODEL", "glm-4-flash"),
			},
			Timeout: durationEnv("MODERATION_TIMEOUT", 4*time.Second),
		},
		Ranking: RankingConfig{
			CoinExtraWeight:    positiveFloatEnv("RANKING_COIN_EXTRA_WEIGHT", 0.35),
			DownloadWeight:     positiveFloatEnv("RANKING_DOWNLOAD_WEIGHT", 0.15),
			FreshnessAmplitude: positiveFloatEnv("RANKING_FRESHNESS_AMPLITUDE", 3.0),
			FreshnessDecayDays: positiveFloatEnv("RANKING_FRESHNESS_DECAY_DAYS", 7.0),
			FeaturedBoost:      positiveFloatEnv("RANKING_FEATURED_BOOST", 1.5),
			JitterBase:         positiveFloatEnv("RANKING_JITTER_BASE", 0.50),
		},
		Retention: RetentionConfig{Unpublished: durationEnv("RETENTION_UNPUBLISHED", 180*24*time.Hour), Audit: durationEnv("RETENTION_AUDIT", 180*24*time.Hour), Feedback: durationEnv("RETENTION_FEEDBACK", 365*24*time.Hour), OrphanBlobs: durationEnv("RETENTION_ORPHAN_BLOBS", 7*24*time.Hour), Interval: durationEnv("RETENTION_INTERVAL", 6*time.Hour)},
		Version:   env("APP_VERSION", "dev"),
		Commit:    env("GIT_COMMIT", ""),
		LogLevel:  env("LOG_LEVEL", "info"),
		LogFormat: env("LOG_FORMAT", "text"),
	}
}

func stringListEnv(key string) []string {
	var values []string
	for _, value := range strings.FieldsFunc(os.Getenv(key), func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if value = strings.TrimRight(strings.TrimSpace(value), "/"); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func (c Config) Validate() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.Storage.LocalRoot == "" {
		errs = append(errs, errors.New("STORAGE_LOCAL_ROOT is required"))
	}
	if c.Limits.UploadMaxBytes <= 0 || c.Limits.PreviewMaxBytes <= 0 || c.Limits.PreviewMaxCount <= 0 {
		errs = append(errs, errors.New("upload limits must be positive"))
	}
	if c.RunningProduction() && len(c.SessionSecret) < 32 {
		errs = append(errs, errors.New("SESSION_SECRET must contain at least 32 characters in production"))
	}
	if c.RunningProduction() && len(c.EncryptionKey) < 32 {
		errs = append(errs, errors.New("TOKEN_ENCRYPTION_KEY must contain at least 32 characters in production"))
	}
	if c.RunningProduction() && (c.BandBBS.ClientID == "" || c.BandBBS.ClientSecret == "" || c.BandBBS.RedirectURI == "") {
		errs = append(errs, errors.New("BANDBBS_CLIENT_ID, BANDBBS_CLIENT_SECRET and BANDBBS_REDIRECT_URI are required in production"))
	}
	if c.RunningProduction() && (len(c.WebClientOrigins) == 0 || slices.Contains(c.WebClientOrigins, "*")) {
		errs = append(errs, errors.New("WEB_CLIENT_ORIGINS must contain explicit trusted origins in production"))
	}
	for _, value := range c.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid CIDR %q", value))
			continue
		}
		// A trusted range decides whether forwarding headers may override the
		// peer address. Anything close to "the whole internet" turns client IP
		// into an attacker-controlled value for rate limits and audit records.
		if c.RunningProduction() && overlyBroadProxyRange(network) {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXY_CIDRS entry %q is too broad to trust in production", value))
		}
	}
	if c.Limits.AuthRatePerMin <= 0 || c.Limits.AuthFailureBurst <= 0 || c.Limits.AuthFailureWindow <= 0 {
		errs = append(errs, errors.New("authentication rate limits must be positive"))
	}

	if c.GitHub.ClientID != "" && (c.GitHub.ClientSecret == "" || c.GitHub.RedirectURI == "") {
		errs = append(errs, errors.New("GITHUB_CLIENT_SECRET and GITHUB_REDIRECT_URI are required when GitHub OAuth is configured"))
	}
	if c.Storage.R2.Enabled {
		if c.Storage.R2.Endpoint == "" || c.Storage.R2.Bucket == "" || c.Storage.R2.AccessKeyID == "" || c.Storage.R2.SecretAccessKey == "" {
			errs = append(errs, errors.New("R2_ENDPOINT, R2_BUCKET, R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY are required when R2 is enabled"))
		}
		if endpoint, err := url.Parse(c.Storage.R2.Endpoint); err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
			errs = append(errs, errors.New("R2_ENDPOINT must be an absolute URL"))
		} else if endpoint.Path != "" && endpoint.Path != "/" {
			errs = append(errs, errors.New("R2_ENDPOINT must not include a bucket name or path; configure R2_BUCKET separately"))
		}
	}
	return errors.Join(errs...)
}

func int64ListEnv(key string) []int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]int64, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			result = append(result, intVal)
		}
	}
	return result
}

func (c Config) RunningProduction() bool { return c.Version != "dev" }

// overlyBroadProxyRange rejects prefixes that cover more addresses than any
// real reverse proxy fleet needs. The thresholds keep room for a /8 style
// private range while refusing 0.0.0.0/0 and similar catch-all entries.
func overlyBroadProxyRange(network *net.IPNet) bool {
	ones, bits := network.Mask.Size()
	if bits == 0 {
		return true
	}
	if bits == 32 {
		return ones < 8
	}
	return ones < 32
}

// ServesHTTPS reports whether the public entrypoint is HTTPS, which gates
// strict transport security and secure cookies.
func (c Config) ServesHTTPS() bool {
	parsed, err := url.Parse(c.PublicURL)
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

func ParseScopes(input string) []string {
	scopes := strings.Fields(input)
	sort.Strings(scopes)
	return scopes
}

func ScopeString(scopes []string) string {
	dup := append([]string(nil), scopes...)
	sort.Strings(dup)
	return strings.Join(dup, " ")
}

func HasScopes(actual string, required []string) bool {
	set := make(map[string]struct{}, len(required))
	for _, scope := range ParseScopes(actual) {
		set[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := set[scope]; !ok {
			return false
		}
	}
	return true
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func int64Env(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// positiveFloatEnv parses a ranking multiplier, rejecting anything that is not
// a positive finite number so invalid values can never reach a SQL literal.
func positiveFloatEnv(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
		return fallback
	}
	return parsed
}

func (c Config) String() string {
	return fmt.Sprintf("addr=%s public_url=%s r2=%t", c.Addr, c.PublicURL, c.Storage.R2.Enabled)
}
