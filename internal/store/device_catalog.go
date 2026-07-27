package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

type catalogDevice struct {
	codename   string
	name       string
	platform   string
	astroboxID string
	vendor     string
}

type contextExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var supportedDeviceCatalog = []catalogDevice{
	{codename: "m67", name: "Xiaomi Smart Band 8 Pro", platform: "vela_os", astroboxID: "m67", vendor: "xiaomi"},
	{codename: "l61", name: "Xiaomi Watch S1 Pro", platform: "vela_os", astroboxID: "l61", vendor: "xiaomi"},
	{codename: "n66", name: "Xiaomi Smart Band 9", platform: "vela_os", astroboxID: "xmb9", vendor: "xiaomi"},
	{codename: "n67", name: "Xiaomi Smart Band 9 Pro", platform: "vela_os", astroboxID: "xmb9p", vendor: "xiaomi"},
	{codename: "o66", name: "Xiaomi Smart Band 10", platform: "vela_os", astroboxID: "xmb10", vendor: "xiaomi"},
	{codename: "o66nfc", name: "Xiaomi Smart Band 10 NFC", platform: "vela_os", astroboxID: "xmb10nfc", vendor: "xiaomi"},
	{codename: "p67", name: "Xiaomi Smart Band 10 Pro", platform: "vela_os", astroboxID: "xmb10p", vendor: "xiaomi"},
	{codename: "n62", name: "Xiaomi Watch S3", platform: "vela_os", astroboxID: "xmws3", vendor: "xiaomi"},
	{codename: "o62", name: "Xiaomi Watch S4", platform: "vela_os", astroboxID: "xmws4", vendor: "xiaomi"},
	{codename: "o62m", name: "Xiaomi Watch S4 15th Anniversary", platform: "vela_os", astroboxID: "xmws4xring", vendor: "xiaomi"},
	{codename: "o63", name: "Xiaomi Watch S4 41mm", platform: "vela_os", astroboxID: "xmws441", vendor: "xiaomi"},
	{codename: "p62", name: "Xiaomi Watch S5", platform: "vela_os", astroboxID: "xmws5", vendor: "xiaomi"},
	{codename: "n65", name: "REDMI Watch 4", platform: "vela_os", astroboxID: "xmrw4", vendor: "redmi"},
	{codename: "o65", name: "REDMI Watch 5", platform: "vela_os", astroboxID: "xmrw5", vendor: "redmi"},
	{codename: "o65m", name: "REDMI Watch 5 eSIM", platform: "vela_os", astroboxID: "xmrw5xring", vendor: "redmi"},
	{codename: "p65", name: "REDMI Watch 6", platform: "vela_os", astroboxID: "xmrw6", vendor: "redmi"},
	{codename: "helio-ring", name: "Amazfit Helio Ring", platform: "zepp_os"},
	{codename: "helio-strap", name: "Amazfit Helio Strap", platform: "zepp_os"},
	{codename: "active", name: "Amazfit Active", platform: "zepp_os"},
	{codename: "active-edge", name: "Amazfit Active Edge", platform: "zepp_os"},
	{codename: "active-max", name: "Amazfit Active Max", platform: "zepp_os"},
	{codename: "active-2-nfc-round", name: "Amazfit Active 2 NFC Round", platform: "zepp_os"},
	{codename: "active-2-round", name: "Amazfit Active 2 Round", platform: "zepp_os"},
	{codename: "active-2-square", name: "Amazfit Active 2 Square", platform: "zepp_os"},
	{codename: "active-3-premium", name: "Amazfit Active 3 Premium", platform: "zepp_os"},
	{codename: "balance", name: "Amazfit Balance", platform: "zepp_os"},
	{codename: "balance-2", name: "Amazfit Balance 2", platform: "zepp_os"},
	{codename: "balance-2-xt", name: "Amazfit Balance 2 XT", platform: "zepp_os"},
	{codename: "band-7", name: "Amazfit Band 7", platform: "zepp_os"},
	{codename: "mi-band-7", name: "Xiaomi Smart Band 7", platform: "zepp_os"},
	{codename: "bip-5", name: "Amazfit Bip 5", platform: "zepp_os"},
	{codename: "bip-5-unity", name: "Amazfit Bip 5 Unity", platform: "zepp_os"},
	{codename: "bip-6", name: "Amazfit Bip 6", platform: "zepp_os"},
	{codename: "cheetah-pro", name: "Amazfit Cheetah Pro", platform: "zepp_os"},
	{codename: "cheetah-round", name: "Amazfit Cheetah R", platform: "zepp_os"},
	{codename: "cheetah-square", name: "Amazfit Cheetah S", platform: "zepp_os"},
	{codename: "cheetah-2-pro", name: "Amazfit Cheetah 2 Pro", platform: "zepp_os"},
	{codename: "falcon", name: "Amazfit Falcon", platform: "zepp_os"},
	{codename: "gtr-3", name: "Amazfit GTR 3", platform: "zepp_os"},
	{codename: "gtr-3-pro", name: "Amazfit GTR 3 Pro", platform: "zepp_os"},
	{codename: "gtr-4", name: "Amazfit GTR 4", platform: "zepp_os"},
	{codename: "gtr-mini", name: "Amazfit GTR Mini", platform: "zepp_os"},
	{codename: "gts-3", name: "Amazfit GTS 3", platform: "zepp_os"},
	{codename: "gts-4", name: "Amazfit GTS 4", platform: "zepp_os"},
	{codename: "gts-4-mini", name: "Amazfit GTS 4 Mini", platform: "zepp_os"},
	{codename: "gts-4-mini-new", name: "Amazfit GTS 4 Mini New", platform: "zepp_os"},
	{codename: "trex-2", name: "Amazfit T-Rex 2", platform: "zepp_os"},
	{codename: "trex-3", name: "Amazfit T-Rex 3", platform: "zepp_os"},
	{codename: "trex-3-pro-44", name: "Amazfit T-Rex 3 Pro 44mm", platform: "zepp_os"},
	{codename: "trex-3-pro-48", name: "Amazfit T-Rex 3 Pro 48mm", platform: "zepp_os"},
	{codename: "trex-ultra", name: "Amazfit T-Rex Ultra", platform: "zepp_os"},
	{codename: "trex-ultra-2", name: "Amazfit T-Rex Ultra 2", platform: "zepp_os"},
}

func seedDevices(ctx context.Context, db contextExecer) error {
	for _, device := range supportedDeviceCatalog {
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://oronbox.org/devices/"+device.codename))
		if _, err := db.ExecContext(ctx, `INSERT INTO devices(id,codename,display_name,platform,astrobox_id,vendor) VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(codename) DO UPDATE SET display_name=excluded.display_name,platform=excluded.platform,astrobox_id=excluded.astrobox_id,vendor=excluded.vendor`, id, device.codename, device.name, device.platform, device.astroboxID, device.vendor); err != nil {
			return err
		}
	}
	return nil
}
