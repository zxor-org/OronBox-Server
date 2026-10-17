package server

import (
	"context"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/observability"
)

func (a *App) RunConsoleSync(ctx context.Context) {
	log := observability.For("console")
	ensure := func() {
		if err := a.consoleManager().Ensure(ctx); err != nil && ctx.Err() == nil {
			log.Error("console initialization failed", "error", err)
		}
	}
	ensure()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ensure()
		}
	}
}
