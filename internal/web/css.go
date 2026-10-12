package web

import _ "embed"

// CSS contains the server console stylesheet. Keeping it as an embedded asset
// prevents the web package from accumulating a generated, thousand-line Go
// string alongside its template code.
//
//go:embed assets/app.css
var CSS string
