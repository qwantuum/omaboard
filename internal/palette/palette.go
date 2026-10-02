package palette

import (
	"os"
	"path/filepath"
	"strings"
)

type Palette struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Dark    bool   `json:"dark"`
	Window  string `json:"window"`
	Surface string `json:"surface"`
	Canvas  string `json:"canvas"`
	FG      string `json:"fg"`
	Muted   string `json:"muted"`
	Accent  string `json:"accent"`
	Border  string `json:"border"`
	Hover   string `json:"hover"`
}

// Mode identifiers in the same order as ModeNames().
const (
	ModeAuto    = "auto"
	ModeGruvbox = "gruvbox"
	ModeNord    = "nord"
	ModeDark    = "dark"
	ModeLight   = "light"
)

var modes = []struct {
	slug, name string
}{
	{ModeAuto, "Следовать Omarchy"},
	{ModeGruvbox, "Gruvbox"},
	{ModeNord, "Nord"},
	{ModeDark, "Тёмная"},
	{ModeLight, "Светлая"},
}

func ModeNames() []string {
	out := make([]string, len(modes))
	for i, m := range modes {
		out[i] = m.name
	}
	return out
}

func ModeIndex(slug string) int {
	for i, m := range modes {
		if m.slug == slug {
			return i
		}
	}
	return 0
}

func ModeSlug(index int) string {
	if index < 0 || index >= len(modes) {
		return ModeAuto
	}
	return modes[index].slug
}

func Gruvbox() *Palette {
	return &Palette{
		Name: "Gruvbox", Slug: ModeGruvbox, Dark: true,
		Window: "#161616", Surface: "#282828", Canvas: "#1e1e1e",
		FG: "#d4be98", Muted: "#a89984", Accent: "#fe8019",
		Border: "#3c3836", Hover: "#3c3836",
	}
}

func Nord() *Palette {
	return &Palette{
		Name: "Nord", Slug: ModeNord, Dark: true,
		Window: "#191c23", Surface: "#2e3440", Canvas: "#222730",
		FG: "#d8dee9", Muted: "#adb5c4", Accent: "#88c0d0",
		Border: "#3b4252", Hover: "#3b4252",
	}
}

func Dark() *Palette {
	return &Palette{
		Name: "Тёмная", Slug: ModeDark, Dark: true,
		Window: "#1e1e1e", Surface: "#2d2d2d", Canvas: "#252525",
		FG: "#ffffff", Muted: "#b5b5b5", Accent: "#3584e4",
		Border: "#3f3f3f", Hover: "#3f3f3f",
	}
}

func Light() *Palette {
	return &Palette{
		Name: "Светлая", Slug: ModeLight, Dark: false,
		Window: "#f6f5f4", Surface: "#ffffff", Canvas: "#ffffff",
		FG: "#1e1e1e", Muted: "#5e5c64", Accent: "#1c71d8",
		Border: "#d6d3d1", Hover: "#ebebeb",
	}
}

// Resolve returns the palette for a mode slug. ModeAuto follows the current
// Omarchy theme and falls back to Dark when Omarchy is not present.
func Resolve(mode string) *Palette {
	switch mode {
	case ModeGruvbox:
		return Gruvbox()
	case ModeNord:
		return Nord()
	case ModeLight:
		return Light()
	case ModeDark:
		return Dark()
	default: // auto
		if name, ok := OmarchyThemeName(); ok {
			if p, ok := OmarchyPalette(name); ok {
				return p
			}
		}
		return Dark()
	}
}

// OmarchyThemeName reads the slug of the currently applied Omarchy theme.
func OmarchyThemeName() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(home, ".local", "state", "omarchy", "current", "theme.name"))
	if err != nil {
		return "", false
	}
	slug := strings.TrimSpace(string(b))
	if slug == "" {
		return "", false
	}
	return slug, true
}

// OmarchyColorsFiles returns candidate colors.toml paths, user overlay first.
func OmarchyColorsFiles(slug string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".config", "omarchy", "themes", slug, "colors.toml"),
		filepath.Join("/usr/share/omarchy/themes", slug, "colors.toml"),
	}
}

// OmarchyColorsPath returns the first existing colors.toml for the theme.
func OmarchyColorsPath(slug string) string {
	for _, path := range OmarchyColorsFiles(slug) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// OmarchyPalette builds a palette from the theme's colors.toml. The user
// overlay in ~/.config/omarchy wins over the stock theme directory.
func OmarchyPalette(slug string) (*Palette, bool) {
	var raw map[string]string
	for _, path := range OmarchyColorsFiles(slug) {
		if b, err := os.ReadFile(path); err == nil {
			raw = parseFlatTOML(string(b))
			if len(raw) > 0 {
				break
			}
		}
	}
	if raw == nil {
		return nil, false
	}
	dark := raw["mode"] != "light"
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}
	p := &Palette{
		Name:   titleize(slug),
		Slug:   slug,
		Dark:   dark,
		FG:     get("foreground"),
		Muted:  get("muted", "dark_foreground"),
		Accent: get("accent", "blue"),
	}
	if dark {
		p.Window = get("darker_background", "background")
		p.Surface = get("background")
		p.Canvas = get("dark_background", "background")
		p.Border = get("selection", "lighter_background")
		p.Hover = get("lighter_background", "selection")
	} else {
		p.Window = get("darker_background", "background")
		p.Surface = get("dark_background", "background")
		p.Canvas = get("background")
		p.Border = get("selection", "darker_background")
		p.Hover = get("lighter_background", "dark_background")
	}
	if p.Window == "" || p.FG == "" {
		return nil, false
	}
	return p, true
}

// OmarchyThemePath is the file watched for theme switches (may not exist).
func OmarchyThemePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "omarchy", "current", "theme.name")
}

// parseFlatTOML extracts key = "value" pairs; enough for colors.toml.
func parseFlatTOML(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		val = strings.Trim(val, `"'`)
		if key != "" && val != "" {
			out[key] = val
		}
	}
	return out
}

func titleize(slug string) string {
	parts := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// OnAccent returns a readable text color for elements painted with Accent.
func (p *Palette) OnAccent() string {
	r, g, b := HexRGB(p.Accent)
	lum := 0.2126*r + 0.7152*g + 0.0722*b
	if lum > 0.55 {
		return "#000000"
	}
	return "#ffffff"
}

// HexRGB converts "#rrggbb" to 0..1 components.
func HexRGB(c string) (float64, float64, float64) {
	c = strings.TrimPrefix(strings.TrimSpace(c), "#")
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	if len(c) != 6 {
		return 0, 0, 0
	}
	var v [3]float64
	for i := 0; i < 3; i++ {
		v[i] = (hexVal(c[i*2])*16 + hexVal(c[i*2+1])) / 255
	}
	return v[0], v[1], v[2]
}

func hexVal(b byte) float64 {
	switch {
	case b >= '0' && b <= '9':
		return float64(b - '0')
	case b >= 'a' && b <= 'f':
		return float64(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return float64(b-'A') + 10
	}
	return 0
}
