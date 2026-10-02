package ui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"omaboard/internal/palette"
)

// cssManager owns the single global GTK CSS provider so the theme can be
// swapped at runtime by reloading the same provider.
type cssManager struct {
	provider *gtk.CSSProvider
}

func newCSS() *cssManager {
	p := gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(
		gdk.DisplayGetDefault(), p, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION,
	)
	return &cssManager{provider: p}
}

func (c *cssManager) apply(p *palette.Palette) {
	c.provider.LoadFromString(buildCSS(p))
}

func buildCSS(p *palette.Palette) string {
	onAccent := p.OnAccent()
	return fmt.Sprintf(`
window {
  background-color: %[1]s;
  color: %[2]s;
}
headerbar {
  background-color: %[3]s;
  color: %[2]s;
  border-bottom: 1px solid %[7]s;
  box-shadow: none;
}
.toolbar {
  background-color: %[3]s;
  border-bottom: 1px solid %[7]s;
  padding: 6px 8px;
}
.statusbar {
  background-color: %[3]s;
  border-top: 1px solid %[7]s;
  padding: 4px 10px;
}
.muted { color: %[4]s; }
.subtle { color: %[4]s; font-size: 13px; }
.app-title { font-size: 34px; font-weight: 800; letter-spacing: 1px; }
.card-title { font-size: 16px; font-weight: 700; }
.card {
  background-color: %[3]s;
  border: 1px solid %[7]s;
  border-radius: 14px;
  padding: 14px 16px;
  min-height: 56px;
}
.card:hover {
  background-color: %[8]s;
  border-color: %[6]s;
}
button {
  background-color: transparent;
  color: %[2]s;
  border: 1px solid transparent;
  border-radius: 8px;
  padding: 5px 12px;
}
button:hover { background-color: %[8]s; }
togglebutton:checked {
  background-color: %[6]s;
  color: %[9]s;
  border-color: transparent;
  font-weight: 600;
}
togglebutton:checked:hover { background-color: %[6]s; }
entry {
  background-color: %[8]s;
  color: %[2]s;
  border: 1px solid %[7]s;
  border-radius: 8px;
  padding: 5px 8px;
}
spinbutton {
  background-color: %[8]s;
  color: %[2]s;
  border: 1px solid %[7]s;
  border-radius: 8px;
}
spinbutton entry { background: transparent; border: none; padding: 2px; }
spinbutton button { background: transparent; border: none; }
spinbutton button:hover { background-color: %[7]s; }
combobox button {
  background-color: %[8]s;
  color: %[2]s;
  border: 1px solid %[7]s;
  border-radius: 8px;
  padding: 4px 8px;
}
colorbutton {
  border: 1px solid %[7]s;
  border-radius: 8px;
  padding: 3px;
}
popover > contents {
  background-color: %[3]s;
  border: 1px solid %[7]s;
  border-radius: 12px;
}
separator { background-color: %[7]s; min-width: 1px; min-height: 1px; }
checkbutton { color: %[2]s; }
url { color: %[6]s; }
`,
		p.Window,  // 1
		p.FG,      // 2
		p.Surface, // 3
		p.Muted,   // 4
		p.Canvas,  // 5
		p.Accent,  // 6
		p.Border,  // 7
		p.Hover,   // 8
		onAccent,  // 9
	)
}
