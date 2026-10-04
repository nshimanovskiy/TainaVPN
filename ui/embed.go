// Package ui holds the shared HTML interface used by the desktop and Android apps.
package ui

import "embed"

// Everything the page loads must be embedded here (the desktop apps serve only these files).
//
//go:embed *.html *.js *.css flags
var FS embed.FS
