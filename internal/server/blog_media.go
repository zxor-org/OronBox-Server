package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/gitea"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
)

// Blog media stays in the configured Gitea content repository, never blobs/R2.
func (a *App) handleBlogMedia(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 4<<20 {
		httpx.WriteJSON(w, 413, httpx.ErrorBody("invalid_image", "image exceeds 4 MiB"))
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || (format != "png" && format != "jpeg" && format != "gif" && format != "webp") {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_image", "unsupported image or dimensions exceed 4096px"))
		return
	}
	sum := sha256.Sum256(data)
	file := "articles/media/" + hex.EncodeToString(sum[:]) + "." + format
	c := gitea.Client{Config: a.cfg.DeviceCatalog}
	head, err := c.Head(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", "content repository unavailable"))
		return
	}
	old, _, err := c.File(r.Context(), file, head)
	var upstream gitea.Error
	switch {
	case err == nil:
		if !bytes.Equal(old, data) {
			httpx.WriteJSON(w, 409, httpx.ErrorBody("image_conflict", "content identity conflict"))
			return
		}
	case errors.As(err, &upstream) && upstream.Status == 404:
		if err = c.Commit(r.Context(), "Add article media "+hex.EncodeToString(sum[:]), []gitea.Change{gitea.FileChange(file, "", data)}); err != nil {
			httpx.WriteJSON(w, 409, httpx.ErrorBody("repository_conflict", "could not save article image; retry"))
			return
		}
	default:
		httpx.WriteJSON(w, 502, httpx.ErrorBody("repository_error", "content repository unavailable"))
		return
	}
	rawURL := strings.TrimRight(c.Config.BaseURL, "/") + "/" + c.Config.Repository + "/raw/branch/" + url.PathEscape(c.Config.Ref) + "/" + file
	a.audit(r, "blog.gitea.media", "success", file)
	httpx.WriteJSON(w, 201, map[string]string{"url": rawURL, "path": file, "sha256": hex.EncodeToString(sum[:])})
}
