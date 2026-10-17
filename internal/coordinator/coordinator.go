package coordinator

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	bandbbsoauth "github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/publish/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

const interval = 3 * time.Second

const pollInterval = 5 * time.Minute

type Coordinator struct {
	db       *sql.DB
	store    *store.Store
	local    blob.Store
	r2       *blob.R2
	secrets  *auth.Secrets
	cfg      config.Config
	bandAuth *bandbbsoauth.Client
	band     *bandbbs.Client
	astro    *astrobox.Client
	log      *slog.Logger
}

func New(db *sql.DB, storage *store.Store, local blob.Store, r2 *blob.R2, secrets *auth.Secrets, cfg config.Config, bandAuth *bandbbsoauth.Client) *Coordinator {
	return &Coordinator{
		db: db, store: storage, local: local, r2: r2, secrets: secrets, cfg: cfg, bandAuth: bandAuth,
		band:  bandbbs.New(cfg.BandBBS.APIURL, local, storage),
		astro: astrobox.New(cfg.AstroBox, cfg.GitHub.APIURL, local, storage),
		log:   observability.For("coordinator"),
	}
}

func (c *Coordinator) Run(ctx context.Context) {
	c.log.Info("resource coordinator started", "r2_enabled", c.r2 != nil)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer c.log.Info("resource coordinator stopped")
	nextMaintenance := time.Time{}
	for {
		c.tick(ctx)
		if time.Now().After(nextMaintenance) {
			if err := c.maintain(ctx); err != nil && !errors.Is(err, context.Canceled) {
				c.log.Error("resource maintenance failed", "error", err)
			}
			nextMaintenance = time.Now().Add(c.cfg.Retention.Interval)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Coordinator) tick(ctx context.Context) {
	if c.r2 != nil {
		if err := c.pruneR2Replica(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
			c.log.Error("R2 visibility reconciliation failed", "error", err)
		}
		if err := c.replicateOne(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
			c.log.Error("R2 replication failed", "error", err)
		}
	}
	if err := c.publishOne(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
		c.log.Error("resource publication failed", "error", err)
	}
	if err := c.pollOne(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
		c.log.Error("AstroBox publication status check failed", "error", err)
	}
}
