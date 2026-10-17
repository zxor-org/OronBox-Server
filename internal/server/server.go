package server

import (
	"net"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/attestation"
	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/consoleupdate"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	githuboauth "github.com/zxor-org/OronBox-Server/internal/oauth/github"
	adminhttp "github.com/zxor-org/OronBox-Server/internal/server/admin"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

type Dependencies struct {
	Config     config.Config
	Store      *store.Store
	BandBBS    *bandbbs.Client
	GitHub     *githuboauth.Client
	StartedAt  time.Time
	Blobs      blob.Store
	Creator    *creator.Service
	R2         *blob.R2
	Moderation *moderation.Service
}

type App struct {
	console            *consoleupdate.Manager
	consoleOnce        sync.Once
	admin              *adminhttp.Handler
	adminOnce          sync.Once
	runtime            runtimeState
	content            contentCache
	releaseSyncMu      sync.Mutex
	cfg                config.Config
	store              *store.Store
	bandbbs            *bandbbs.Client
	github             *githuboauth.Client
	startedAt          time.Time
	secrets            *authcore.Secrets
	blobs              blob.Store
	creator            *creator.Service
	r2                 *blob.R2
	moderation         *moderation.Service
	templates          *web.Templates
	trustedProxies     []*net.IPNet
	downloadLimiter    *ipRateLimiter
	authAttempts       *ipRateLimiter
	authFailures       *ipRateLimiter
	attestationOptions attestation.Options
}

func New(deps Dependencies) *App {
	secrets, err := authcore.NewSecrets(deps.Config.EncryptionKey)
	if err != nil {
		panic(err)
	}
	app := &App{
		cfg:             deps.Config,
		store:           deps.Store,
		bandbbs:         deps.BandBBS,
		github:          deps.GitHub,
		startedAt:       deps.StartedAt,
		secrets:         secrets,
		blobs:           deps.Blobs,
		creator:         deps.Creator,
		r2:              deps.R2,
		moderation:      deps.Moderation,
		templates:       web.NewTemplates(),
		downloadLimiter: newIPRateLimiter(deps.Config.Limits.DownloadRatePerMin),
		authAttempts:    newWindowLimiter(deps.Config.Limits.AuthRatePerMin, time.Minute, 120),
		authFailures:    newWindowLimiter(deps.Config.Limits.AuthFailureBurst, deps.Config.Limits.AuthFailureWindow, 20),
	}
	if app.creator != nil {
		app.creator.VerifyImportedBinding = app.verifyImportedBinding
	}
	app.attestationOptions = attestation.Options{MasterKey: deps.Config.AttestationMasterKey, NonceStore: attestation.NewNonceStore(2 * attestation.DefaultSkew), MinimumVersion: deps.Config.MinimumClientVersion, Revocation: attestation.NewRevocation(deps.Config.RevokedClientKeys)}
	for _, value := range deps.Config.TrustedProxyCIDRs {
		if _, network, parseErr := net.ParseCIDR(value); parseErr == nil {
			app.trustedProxies = append(app.trustedProxies, network)
		}
	}
	return app
}
