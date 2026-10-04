package ui

import (
	"io/fs"
	"regexp"
	"testing"
)

// Every file referenced by index.html (scripts, styles) must be embedded,
// otherwise the desktop apps show a broken page.
func TestEmbeddedAssets(t *testing.T) {
	html, err := fs.ReadFile(FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`(?:src|href)="([^"#?:]+)"`).FindAllStringSubmatch(string(html), -1)
	if len(refs) == 0 {
		t.Fatal("no references found")
	}
	for _, m := range refs {
		if _, err := fs.Stat(FS, m[1]); err != nil {
			t.Errorf("index.html references %q but it is not embedded", m[1])
		}
	}
	for _, f := range []string{"app.js", "i18n.js", "style.css", "flags/de.svg", "flags/xx.svg"} {
		if _, err := fs.Stat(FS, f); err != nil {
			t.Errorf("%s is not embedded", f)
		}
	}
}
