// Package ui holds the shared HTML interface used by the desktop and Android apps.
package ui

import "embed"

//go:embed index.html app.js style.css flags
var FS embed.FS
