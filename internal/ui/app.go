package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"omaboard/internal/palette"
)

type Config struct {
	Theme  string  `json:"theme"`
	Color  string  `json:"color"`
	Stroke float64 `json:"stroke"`
	NoGrid bool    `json:"nogrid,omitempty"`
}

type App struct {
	gapp *gtk.Application
	win  *gtk.ApplicationWindow
	cfg  Config
	pal  *palette.Palette
	css  *cssManager

	menuBox   *gtk.Box
	infoLabel *gtk.Label
	combos    []*gtk.ComboBoxText
	updating  bool

	board     *BoardSession
	lastSig   string
	startURL  string // --connect target
	startMode string // "" | "server"
	port      int
}

// Run boots the GTK application. startURL != "" opens a remote board
// directly, startMode "server" skips the menu too.
func Run(args []string, startURL, startMode string, port int) int {
	a := &App{cfg: loadConfig(), startURL: startURL, startMode: startMode, port: port}
	if a.cfg.Stroke <= 0 {
		a.cfg.Stroke = 3
	}
	if a.cfg.Theme == "" {
		a.cfg.Theme = palette.ModeAuto
	}

	a.gapp = gtk.NewApplication("dev.omaboard.Omaboard", 0)
	a.gapp.ConnectActivate(func() {
		if a.win == nil {
			// GTK is initialized by Run, so display-dependent setup happens here.
			a.css = newCSS()
			a.pal = palette.Resolve(a.cfg.Theme)
			a.css.apply(a.pal)

			a.buildWindow()
			a.startPoller()
			if a.startURL != "" {
				a.openBoard("client", a.startURL)
			} else if a.startMode == "server" {
				a.openBoard("server", "")
			}
		}
		a.win.Present()
	})
	return a.gapp.Run(args)
}

func (a *App) buildWindow() {
	a.win = gtk.NewApplicationWindow(a.gapp)
	a.win.SetTitle("Omaboard")
	a.win.SetDefaultSize(470, 560)

	a.menuBox = a.buildMenu()
	a.win.SetChild(a.menuBox)

	a.win.ConnectCloseRequest(func() bool {
		if a.board != nil {
			a.board.teardown()
			a.board = nil
			a.showMenu()
			return true // keep the window alive, go back to the menu
		}
		return false // no session: close and quit
	})
}

// ---------- menu ----------

func (a *App) buildMenu() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 16)
	box.SetHAlign(gtk.AlignCenter)
	box.SetVAlign(gtk.AlignCenter)
	box.SetHExpand(true)
	box.SetVExpand(true)
	box.SetMarginTop(36)
	box.SetMarginBottom(36)
	box.SetMarginStart(32)
	box.SetMarginEnd(32)

	title := gtk.NewLabel("Omaboard")
	title.AddCSSClass("app-title")
	sub := gtk.NewLabel("Интерактивная доска на GTK")
	sub.AddCSSClass("muted")
	box.Append(title)
	box.Append(sub)

	box.Append(a.card("Создать локальную доску",
		"Обычное рисование, ничего не покинет компьютер",
		func() { a.openBoard("local", "") }))

	box.Append(a.card("Создать серверную (мультиплеер) доску",
		"Поднять сервер: к доске можно подключиться из браузера",
		func() { a.openBoard("server", "") }))

	// theme row
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
	row.SetHAlign(gtk.AlignCenter)
	row.Append(gtk.NewLabel("Тема:"))
	combo := gtk.NewComboBoxText()
	for _, name := range palette.ModeNames() {
		combo.AppendText(name)
	}
	combo.SetActive(palette.ModeIndex(a.cfg.Theme))
	combo.ConnectChanged(func() {
		if a.updating {
			return
		}
		a.setThemeMode(palette.ModeSlug(combo.Active()))
	})
	a.combos = append(a.combos, combo)
	row.Append(combo)
	box.Append(row)

	a.infoLabel = gtk.NewLabel("")
	a.infoLabel.AddCSSClass("subtle")
	a.infoLabel.SetWrap(true)
	a.infoLabel.SetMaxWidthChars(44)
	box.Append(a.infoLabel)
	a.updateMenuInfo()

	return box
}

func (a *App) card(title, subtitle string, onClick func()) *gtk.Button {
	btn := gtk.NewButton()
	inner := gtk.NewBox(gtk.OrientationVertical, 4)
	l1 := gtk.NewLabel(title)
	l1.AddCSSClass("card-title")
	l1.SetHAlign(gtk.AlignStart)
	l2 := gtk.NewLabel(subtitle)
	l2.AddCSSClass("muted")
	l2.SetHAlign(gtk.AlignStart)
	l2.SetWrap(true)
	inner.Append(l1)
	inner.Append(l2)
	btn.SetChild(inner)
	btn.AddCSSClass("card")
	btn.SetHExpand(true)
	btn.ConnectClicked(onClick)
	return btn
}

func (a *App) showMenu() {
	a.win.SetTitle("Omaboard")
	a.win.SetTitlebar(nil)
	a.win.SetChild(a.menuBox)
	a.win.SetDefaultSize(470, 560)
}

func (a *App) updateMenuInfo() {
	if a.infoLabel == nil {
		return
	}
	if slug, ok := palette.OmarchyThemeName(); ok {
		if a.cfg.Theme == palette.ModeAuto {
			a.infoLabel.SetText(fmt.Sprintf("Синхронизация с Omarchy: тема «%s»", slug))
		} else {
			a.infoLabel.SetText(fmt.Sprintf("Omarchy: «%s» · выбрана тема вручную", slug))
		}
		return
	}
	if a.cfg.Theme == palette.ModeAuto {
		a.infoLabel.SetText("Omarchy не найден — используется тёмная тема")
	} else {
		a.infoLabel.SetText("Omarchy не найден")
	}
}

// ---------- theme ----------

func (a *App) setThemeMode(slug string) {
	a.cfg.Theme = slug
	saveConfig(a.cfg)
	a.applyPalette(palette.Resolve(slug))
	a.updateCombos()
}

func (a *App) updateCombos() {
	a.updating = true
	idx := palette.ModeIndex(a.cfg.Theme)
	for _, c := range a.combos {
		if c.Active() != idx {
			c.SetActive(idx)
		}
	}
	a.updating = false
}

func (a *App) applyPalette(p *palette.Palette) {
	a.pal = p
	a.css.apply(p)
	if a.board != nil {
		a.board.onPalette(p)
	}
	a.updateMenuInfo()
}

// startPoller follows Omarchy theme switches while mode is "auto".
func (a *App) startPoller() {
	a.lastSig = themeSignature()
	go func() {
		for range time.Tick(3 * time.Second) {
			sig := themeSignature()
			glib.IdleAdd(func() {
				if a.cfg.Theme != palette.ModeAuto {
					a.lastSig = sig
					return
				}
				if sig != a.lastSig {
					a.lastSig = sig
					a.applyPalette(palette.Resolve(palette.ModeAuto))
				}
			})
		}
	}()
}

func themeSignature() string {
	slug, ok := palette.OmarchyThemeName()
	if !ok {
		return ""
	}
	sig := slug
	if path := palette.OmarchyColorsPath(slug); path != "" {
		if st, err := os.Stat(path); err == nil {
			sig += fmt.Sprintf(":%d", st.ModTime().UnixNano())
		}
	}
	return sig
}

// ---------- config ----------

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "omaboard", "config.json")
}

func loadConfig() Config {
	cfg := Config{Theme: palette.ModeAuto, Stroke: 3}
	path := configPath()
	if path == "" {
		return cfg
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var stored Config
	if err := json.Unmarshal(b, &stored); err != nil {
		return cfg
	}
	if stored.Theme != "" {
		cfg.Theme = stored.Theme
	}
	cfg.Color = stored.Color
	if stored.Stroke > 0 {
		cfg.Stroke = stored.Stroke
	}
	cfg.NoGrid = stored.NoGrid
	return cfg
}

func saveConfig(cfg Config) {
	path := configPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("omaboard: config dir: %v", err)
		return
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Printf("omaboard: config save: %v", err)
	}
}
