package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/coordinator"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	githuboauth "github.com/zxor-org/OronBox-Server/internal/oauth/github"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/server"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	observability.Configure(cfg.LogLevel, cfg.LogFormat)
	log := observability.For("server")
	log.Info("starting OronBox Server", "version", cfg.Version, "commit", cfg.Commit, "log_format", cfg.LogFormat, "log_level", cfg.LogLevel)
	if err := cfg.Validate(); err != nil {
		log.Error("configuration validation failed", "error", err)
		os.Exit(1)
	}
	// The bootstrap admin list bypasses the users.role check, so which BandBBS
	// accounts it covers has to be visible in the startup record rather than
	// only in whoever's shell set the variable.
	log.Info("admin access configured",
		"bootstrap_bandbbs_user_ids", cfg.Admin.BandBBSUserIDs,
		"public_url", cfg.PublicURL,
		"https", cfg.ServesHTTPS(),
		"trusted_proxy_cidrs", cfg.TrustedProxyCIDRs,
	)

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := store.Migrate(context.Background(), db); err != nil {
		log.Error("database schema setup failed", "error", err)
		os.Exit(1)
	}
	log.Info("database ready")

	s := store.New(db)
	blobs, err := blob.NewLocal(cfg.Storage.LocalRoot)
	if err != nil {
		log.Error("local blob storage initialization failed", "error", err)
		os.Exit(1)
	}
	log.Info("local blob storage ready", "root", cfg.Storage.LocalRoot, "upload_limit_bytes", cfg.Limits.UploadMaxBytes, "media_limit_bytes", cfg.Limits.PreviewMaxBytes)
	bandBBSOAuth := bandbbs.NewClient(cfg.BandBBS, 15*time.Second)
	creatorService := creator.New(db, blobs, creator.Limits{UploadMaxBytes: cfg.Limits.UploadMaxBytes, PreviewMaxBytes: cfg.Limits.PreviewMaxBytes, PreviewMaxCount: cfg.Limits.PreviewMaxCount})
	creatorService.Ranking = creator.Ranking{
		CoinExtraWeight:    cfg.Ranking.CoinExtraWeight,
		DownloadWeight:     cfg.Ranking.DownloadWeight,
		FreshnessAmplitude: cfg.Ranking.FreshnessAmplitude,
		FreshnessDecayDays: cfg.Ranking.FreshnessDecayDays,
		FeaturedBoost:      cfg.Ranking.FeaturedBoost,
		JitterBase:         cfg.Ranking.JitterBase,
	}
	moderationService := moderation.New(
		moderation.Endpoint{Name: "deepseek", BaseURL: cfg.Moderation.Primary.BaseURL, APIKey: cfg.Moderation.Primary.APIKey, Model: cfg.Moderation.Primary.Model},
		moderation.Endpoint{Name: "glm", BaseURL: cfg.Moderation.Fallback.BaseURL, APIKey: cfg.Moderation.Fallback.APIKey, Model: cfg.Moderation.Fallback.Model},
		cfg.Moderation.Timeout,
	)
	secrets, err := auth.NewSecrets(cfg.EncryptionKey)
	if err != nil {
		log.Error("credential encryption initialization failed", "error", err)
		os.Exit(1)
	}
	var r2 *blob.R2
	if cfg.Storage.R2.Enabled {
		r2, err = blob.NewR2(ctx, blob.R2Config{
			Endpoint: cfg.Storage.R2.Endpoint, Region: cfg.Storage.R2.Region,
			Bucket: cfg.Storage.R2.Bucket, AccessKeyID: cfg.Storage.R2.AccessKeyID,
			SecretAccessKey: cfg.Storage.R2.SecretAccessKey,
		})
		if err != nil {
			log.Error("R2 initialization failed", "error", err)
			os.Exit(1)
		}
		log.Info("R2 replica enabled", "bucket", cfg.Storage.R2.Bucket)
	}
	coord := coordinator.New(db, s, blobs, r2, secrets, cfg, bandBBSOAuth)
	creatorService.BandBBSDelete = coord.DeleteBandBBSResources
	creatorService.AstroBoxRemove = coord.RemoveAstroBoxItem
	go coord.Run(ctx)

	app := server.New(server.Dependencies{
		Config:     cfg,
		Store:      s,
		BandBBS:    bandBBSOAuth,
		GitHub:     githuboauth.New(cfg.GitHub),
		StartedAt:  time.Now().UTC(),
		Blobs:      blobs,
		Creator:    creatorService,
		R2:         r2,
		Moderation: moderationService,
	})

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           observability.HTTP(server.CORS(cfg.WebClientOrigins, server.SecurityHeaders(cfg, app.Routes()))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("OronBox Server is ready", "address", cfg.Addr, "public_url", cfg.PublicURL, "r2_enabled", r2 != nil)
		serverErrors <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		log.Info("shutting down OronBox Server", "reason", ctx.Err())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP shutdown failed", "error", err)
		}
	}
	log.Info("OronBox Server stopped")
}
