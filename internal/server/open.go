package server

import (
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

var compactMAC = regexp.MustCompile(`(?i)^[0-9a-f]{12}$`)

func (a *App) handleOpen(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("source") != "deviceQr" || strings.TrimSpace(query.Get("name")) == "" || !compactMAC.MatchString(query.Get("mac")) {
		http.Error(w, "Invalid OronBox share link", http.StatusBadRequest)
		return
	}
	parameters := url.Values{
		"source": {"deviceQr"},
		"name":   {query.Get("name")},
		"mac":    {strings.ToUpper(query.Get("mac"))},
	}
	if authkey := strings.TrimSpace(query.Get("authkey")); authkey != "" {
		parameters.Set("authkey", authkey)
	}
	deepLink := (&url.URL{Scheme: "oronbox", Host: "open", RawQuery: parameters.Encode()}).String()
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "在 OronBox 中打开",
		Heading:     "在 OronBox 中打开",
		Description: "正在尝试唤起 OronBox，若没有自动打开，请点击下方按钮",
		ButtonLabel: "打开 OronBox",
		Target:      template.URL(deepLink),
		Auto:        true,
		Tone:        "info",
	})
}
