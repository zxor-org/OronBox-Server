package console

import "embed"

// Dist is the compiled admin UI. `npm run build` writes ./dist before
// the Go binary is produced so the console ships inside the server.
//
//go:embed all:dist
var Dist embed.FS
