package admin

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type Dependencies struct {
	Config    config.Config
	Store     *store.Store
	Creator   *creator.Service
	Actor     func(*http.Request) store.AdminSession
	CSRFToken func(*http.Request) string
	ClientIP  func(*http.Request) string
	Audit     func(*http.Request, string, string, string)
}
type Handler struct {
	cfg       config.Config
	store     *store.Store
	creator   *creator.Service
	actor     func(*http.Request) store.AdminSession
	csrfToken func(*http.Request) string
	clientIP  func(*http.Request) string
	audit     func(*http.Request, string, string, string)
}

func New(deps Dependencies) *Handler {
	return &Handler{cfg: deps.Config, store: deps.Store, creator: deps.Creator, actor: deps.Actor, csrfToken: deps.CSRFToken, clientIP: deps.ClientIP, audit: deps.Audit}
}
