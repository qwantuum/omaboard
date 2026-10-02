package ui

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/google/uuid"

	"omaboard/internal/doc"
	"omaboard/internal/draw"
	"omaboard/internal/netx"
	"omaboard/internal/palette"
)

type dragState struct {
	orig  doc.Element
	el    doc.Element
	sx    float64
	sy    float64
	moved bool
}

type BoardSession struct {
	app    *App
	mode   string // local, server, client
	doc    *doc.Document
	hub    *netx.Hub
	client *netx.Client
	url    string

	tool    string
	color   string
	strokeW float64

	panX, panY   float64
	panning      bool
	panSX, panSY float64
	panOX, panOY float64

	current   *doc.Element
	selected  string
	drag      *dragState
	closed    bool
	connState string

	da         *gtk.DrawingArea
	statusInfo *gtk.Label
	shareEntry *gtk.Entry
	toggles    map[string]*gtk.ToggleButton
	colorBtn   *gtk.ColorButton
	keyCtl     *gtk.EventControllerKey
	width      int
	height     int
}

var toolDefs = []struct{ id, label, key string }{
	{"select", "Выбор", "V"},
	{"hand", "Рука", "H"},
	{"pen", "Перо", "P"},
	{"line", "Линия", "L"},
	{"rect", "Прямоугольник", "R"},
	{"ellipse", "Эллипс", "O"},
	{"text", "Текст", "T"},
	{"eraser", "Ластик", "E"},
}

// openBoard builds a fresh session in the requested mode.
func (a *App) openBoard(mode, url string) {
	if a.board != nil {
		a.board.teardown()
		a.board = nil
	}

	s := &BoardSession{
		app:     a,
		mode:    mode,
		doc:     doc.New(),
		tool:    "select",
		color:   a.cfg.Color,
		strokeW: a.cfg.Stroke,
		toggles: map[string]*gtk.ToggleButton{},
	}
	if s.color == "" {
		s.color = a.pal.FG
	}

	switch mode {
	case "server":
		hub := netx.NewHub(a.pal)
		if _, err := hub.Serve(a.port); err != nil {
			hub.Close()
			a.showError("Не удалось запустить сервер", err.Error())
			return
		}
		s.hub = hub
		s.connState = "подключение…"
		if ip := netx.LANIP(); ip != "" {
			s.url = fmt.Sprintf("http://%s:%d", ip, a.port)
		} else {
			s.url = fmt.Sprintf("http://127.0.0.1:%d", a.port)
		}
	case "client":
		s.url = url
		s.connState = "подключение…"
	}

	a.board = s
	s.build()

	// Dial runs off the main thread: a slow network must never freeze GTK.
	switch mode {
	case "server":
		s.dialAsync(fmt.Sprintf("ws://127.0.0.1:%d/ws", a.port))
	case "client":
		s.dialAsync(urlToWS(url))
	}
}

func (s *BoardSession) dialAsync(wsURL string) {
	go func() {
		client, err := netx.Dial(wsURL, s.onRemote)
		glib.IdleAdd(func() {
			if s.closed {
				if client != nil {
					client.Close()
				}
				return
			}
			if err != nil {
				s.connState = "ошибка подключения"
				s.setStatus("Не удалось подключиться: " + err.Error())
				s.app.showError("Ошибка подключения", err.Error())
				return
			}
			s.client = client
			s.connState = "подключено"
			s.updateStatus()
		})
	}()
}

func urlToWS(u string) string {
	u = strings.TrimSpace(strings.TrimRight(u, "/"))
	if strings.HasPrefix(u, "ws://") || strings.HasPrefix(u, "wss://") {
		if !strings.HasSuffix(u, "/ws") {
			u += "/ws"
		}
		return u
	}
	scheme := "ws://"
	if strings.HasPrefix(u, "https://") {
		scheme = "wss://" // behind an HTTPS tunnel (ngrok, cloudflared, ...)
	}
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return scheme + u + "/ws"
}

// ---------- layout ----------

func (s *BoardSession) build() {
	a := s.app

	bar := gtk.NewHeaderBar()
	title := gtk.NewLabel(s.titleText())
	bar.SetTitleWidget(title)

	mb := gtk.NewMenuButton()
	mb.SetChild(gtk.NewImageFromIconName("open-menu-symbolic"))
	mb.SetTooltipText("Меню")
	pop := gtk.NewPopover()
	mb.SetPopover(pop)
	pop.SetChild(s.menuPopover())
	bar.PackEnd(mb)

	root := gtk.NewBox(gtk.OrientationVertical, 0)
	root.Append(s.buildToolbar())
	root.Append(s.buildCanvas())
	root.Append(s.buildStatus())

	kc := gtk.NewEventControllerKey()
	kc.ConnectKeyPressed(func(keyval, keycode uint, state gdk.ModifierType) bool {
		return s.onKey(keyval, state)
	})
	a.win.AddController(kc)
	s.keyCtl = kc

	a.win.SetTitle(s.titleText())
	a.win.SetTitlebar(bar)
	a.win.SetChild(root)
	a.win.SetDefaultSize(1280, 820)

	s.updateStatus()
	s.invalidate()
}

func (s *BoardSession) titleText() string {
	switch s.mode {
	case "server":
		return fmt.Sprintf("Omaboard — серверная доска :%d", s.app.port)
	case "client":
		return "Omaboard — подключён"
	default:
		return "Omaboard — локальная доска"
	}
}

func (s *BoardSession) menuPopover() *gtk.Box {
	a := s.app
	menu := gtk.NewBox(gtk.OrientationVertical, 4)
	menu.SetMarginTop(10)
	menu.SetMarginBottom(10)
	menu.SetMarginStart(10)
	menu.SetMarginEnd(10)

	// theme picker
	row := gtk.NewBox(gtk.OrientationHorizontal, 8)
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
	menu.Append(row)

	// grid toggle
	gridChk := gtk.NewCheckButtonWithLabel("Показывать сетку")
	gridChk.SetActive(!a.cfg.NoGrid)
	gridChk.ConnectToggled(func() {
		if a.updating {
			return
		}
		a.cfg.NoGrid = !gridChk.Active()
		saveConfig(a.cfg)
		s.invalidate()
	})
	menu.Append(gridChk)
	menu.Append(gtk.NewSeparator(gtk.OrientationHorizontal))

	menu.Append(menuAction("Экспорт PNG", s.exportPNG))
	menu.Append(menuAction("Сохранить доску (JSON)", s.saveJSON))
	menu.Append(menuAction("Открыть доску (JSON)", s.loadJSON))
	menu.Append(menuAction("Очистить доску", s.clear))
	menu.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	menu.Append(menuAction("← Вернуться в меню", func() {
		s.app.win.Close() // CloseRequest tears the session down
	}))
	return menu
}

func menuAction(label string, fn func()) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.SetHAlign(gtk.AlignStart)
	b.ConnectClicked(fn)
	return b
}

func (s *BoardSession) buildToolbar() *gtk.Box {
	tb := gtk.NewBox(gtk.OrientationHorizontal, 6)
	tb.AddCSSClass("toolbar")

	linked := gtk.NewBox(gtk.OrientationHorizontal, 0)
	linked.AddCSSClass("linked")
	var first *gtk.ToggleButton
	for _, td := range toolDefs {
		td := td
		b := gtk.NewToggleButtonWithLabel(td.label)
		b.SetTooltipText(fmt.Sprintf("%s (%s)", td.label, td.key))
		if first == nil {
			first = b
			b.SetActive(true)
		} else {
			b.SetGroup(first)
		}
		b.ConnectToggled(func() {
			if b.Active() {
				s.setTool(td.id)
			}
		})
		s.toggles[td.id] = b
		linked.Append(b)
	}
	tb.Append(linked)
	tb.Append(gtk.NewSeparator(gtk.OrientationVertical))

	s.colorBtn = gtk.NewColorButton()
	s.colorBtn.SetTitle("Цвет")
	s.colorBtn.SetRGBA(rgbaOf(s.color))
	s.colorBtn.ConnectColorSet(func() {
		s.color = hexOf(s.colorBtn.RGBA())
		s.app.cfg.Color = s.color
		saveConfig(s.app.cfg)
	})
	tb.Append(s.colorBtn)

	spin := gtk.NewSpinButtonWithRange(1, 32, 1)
	spin.SetValue(s.strokeW)
	spin.SetTooltipText("Толщина линии")
	spin.ConnectValueChanged(func() {
		s.strokeW = spin.Value()
		s.app.cfg.Stroke = s.strokeW
		saveConfig(s.app.cfg)
	})
	tb.Append(spin)
	tb.Append(gtk.NewSeparator(gtk.OrientationVertical))

	undo := gtk.NewButtonWithLabel("↶")
	undo.SetTooltipText("Отменить (Ctrl+Z)")
	undo.ConnectClicked(s.undo)
	redo := gtk.NewButtonWithLabel("↷")
	redo.SetTooltipText("Вернуть (Ctrl+Y)")
	redo.ConnectClicked(s.redo)
	tb.Append(undo)
	tb.Append(redo)

	return tb
}

func (s *BoardSession) buildCanvas() *gtk.DrawingArea {
	da := gtk.NewDrawingArea()
	da.SetHExpand(true)
	da.SetVExpand(true)
	da.SetContentWidth(900)
	da.SetContentHeight(600)
	da.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
		s.draw(cr, w, h)
	})

	click := gtk.NewGestureClick()
	click.SetButton(1)
	click.ConnectPressed(func(n int, x, y float64) { s.onPress(x, y) })
	click.ConnectReleased(func(n int, x, y float64) { s.onRelease() })
	da.AddController(click)

	for _, btn := range []uint{2, 3} {
		pc := gtk.NewGestureClick()
		pc.SetButton(btn)
		pc.ConnectPressed(func(n int, x, y float64) { s.beginPan(x, y) })
		pc.ConnectReleased(func(n int, x, y float64) { s.endPan() })
		da.AddController(pc)
	}

	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, y float64) { s.onMotion(x, y) })
	da.AddController(motion)

	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxes)
	scroll.ConnectScroll(func(dx, dy float64) bool {
		s.onScroll(dx, dy)
		return true
	})
	da.AddController(scroll)
	s.da = da
	return da
}

func (s *BoardSession) buildStatus() *gtk.Box {
	st := gtk.NewBox(gtk.OrientationHorizontal, 8)
	st.AddCSSClass("statusbar")

	s.statusInfo = gtk.NewLabel("")
	s.statusInfo.SetHAlign(gtk.AlignStart)
	s.statusInfo.SetHExpand(true)
	st.Append(s.statusInfo)

	if s.mode != "local" {
		s.shareEntry = gtk.NewEntry()
		s.shareEntry.SetEditable(false)
		s.shareEntry.SetText(s.url)
		s.shareEntry.SetWidthChars(34)
		st.Append(s.shareEntry)

		copyBtn := gtk.NewButtonWithLabel("Копировать")
		copyBtn.ConnectClicked(func() {
			s.app.win.Clipboard().SetText(s.url)
			s.setStatus("Ссылка скопирована — отправьте коллеге")
		})
		st.Append(copyBtn)
	}
	return st
}

// ---------- drawing ----------

func (s *BoardSession) draw(cr *cairo.Context, w, h int) {
	s.width, s.height = w, h
	p := s.app.pal
	draw.Background(cr, p, float64(w), float64(h))
	cr.Save()
	cr.Translate(-s.panX, -s.panY)
	if !s.app.cfg.NoGrid {
		draw.Grid(cr, p, s.panX, s.panY, float64(w), float64(h))
	}
	draw.Elements(cr, s.doc.Elements())
	if s.current != nil {
		draw.Element(cr, *s.current)
	}
	var sel *doc.Element
	if s.drag != nil {
		el := s.drag.el
		sel = &el
	} else if s.selected != "" {
		if el, ok := s.doc.Get(s.selected); ok {
			sel = &el
		}
	}
	if sel != nil {
		draw.Selection(cr, *sel, p)
	}
	cr.Restore()
}

func (s *BoardSession) invalidate() {
	if s.da != nil {
		s.da.QueueDraw()
	}
	s.updateStatus()
}

func (s *BoardSession) setStatus(text string) {
	if s.statusInfo != nil {
		s.statusInfo.SetText(text)
	}
}

func (s *BoardSession) updateStatus() {
	if s.statusInfo == nil {
		return
	}
	names := map[string]string{
		"local":  "Локальная доска",
		"server": "Серверная доска",
		"client": "Удалённая доска",
	}
	base := names[s.mode]
	if s.mode != "local" && s.connState != "" {
		base += " · " + s.connState
	}
	n := s.doc.Len()
	forms := [3]string{"объект", "объекта", "объектов"}
	n10, n100 := n%10, n%100
	word := forms[2]
	if n10 == 1 && n100 != 11 {
		word = forms[0]
	} else if n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14) {
		word = forms[1]
	}
	s.statusInfo.SetText(fmt.Sprintf("%s · %d %s", base, n, word))
}

// ---------- tools ----------

func (s *BoardSession) setTool(id string) {
	if s.tool == id {
		return
	}
	s.tool = id
	s.selected = ""
	s.current = nil
	s.drag = nil
	for name, t := range s.toggles {
		if name == id && !t.Active() {
			t.SetActive(true)
		}
	}
	s.updateCursor()
	s.invalidate()
}

func (s *BoardSession) updateCursor() {
	if s.da == nil {
		return
	}
	if s.panning {
		s.da.SetCursorFromName("grabbing")
	} else if s.tool == "hand" {
		s.da.SetCursorFromName("grab")
	} else {
		s.da.SetCursor(nil)
	}
}

func (s *BoardSession) beginPan(x, y float64) {
	s.current = nil
	s.drag = nil
	s.panning = true
	s.panSX, s.panSY = x, y
	s.panOX, s.panOY = s.panX, s.panY
	s.updateCursor()
	s.invalidate()
}

func (s *BoardSession) endPan() {
	if !s.panning {
		return
	}
	s.panning = false
	s.updateCursor()
	s.invalidate()
}

func (s *BoardSession) onScroll(dx, dy float64) {
	if s.current != nil || s.drag != nil || s.panning {
		return
	}
	s.panX += dx
	s.panY += dy
	s.invalidate()
}

func (s *BoardSession) onPress(x, y float64) {
	if s.tool == "hand" {
		s.beginPan(x, y)
		return
	}
	x += s.panX
	y += s.panY
	switch s.tool {
	case "select":
		if el, ok := draw.HitTest(s.doc.Elements(), x, y); ok {
			s.selected = el.ID
			s.drag = &dragState{orig: el, el: copyEl(el), sx: x, sy: y}
		} else {
			s.selected = ""
			s.drag = nil
		}
		s.invalidate()
	case "eraser":
		if el, ok := draw.HitTest(s.doc.Elements(), x, y); ok {
			s.commit([]doc.Op{doc.DelOp(el)})
			if s.selected == el.ID {
				s.selected = ""
			}
		}
	case "text":
		s.textDialog(x, y)
	default:
		s.current = &doc.Element{
			ID: uuid.NewString(), Type: s.tool,
			Color: s.color, StrokeW: s.strokeW,
			Points: []doc.Point{{X: x, Y: y}, {X: x, Y: y}},
			X:      x, Y: y,
		}
	}
}

func (s *BoardSession) onMotion(x, y float64) {
	if s.panning {
		s.panX = s.panOX - (x - s.panSX)
		s.panY = s.panOY - (y - s.panSY)
		s.invalidate()
		return
	}
	x += s.panX
	y += s.panY
	if s.drag != nil {
		dx, dy := x-s.drag.sx, y-s.drag.sy
		if dx != 0 || dy != 0 {
			s.drag.moved = true
			s.drag.el = copyEl(s.drag.orig)
			draw.Move(&s.drag.el, dx, dy)
			s.invalidate()
		}
		return
	}
	if s.current == nil {
		return
	}
	switch s.current.Type {
	case "pen":
		last := s.current.Points[len(s.current.Points)-1]
		if last.X != x || last.Y != y {
			s.current.Points = append(s.current.Points, doc.Point{X: x, Y: y})
		}
	case "line":
		s.current.Points[1] = doc.Point{X: x, Y: y}
		s.current.W = x - s.current.X
		s.current.H = y - s.current.Y
	default:
		s.current.W = x - s.current.X
		s.current.H = y - s.current.Y
	}
	s.invalidate()
}

func (s *BoardSession) onRelease() {
	if s.panning {
		s.endPan()
		return
	}
	if s.drag != nil {
		if s.drag.moved {
			s.commit([]doc.Op{doc.UpdOp(s.drag.el, s.drag.orig)})
			s.selected = s.drag.el.ID
		}
		s.drag = nil
		s.invalidate()
		return
	}
	if s.current == nil {
		return
	}
	el := *s.current
	s.current = nil

	switch el.Type {
	case "pen":
		if len(el.Points) < 3 {
			s.invalidate()
			return
		}
		el.Points = dedupe(el.Points)
	case "line":
		if el.W == 0 && el.H == 0 {
			s.invalidate()
			return
		}
		el.Points = []doc.Point{el.Points[0], {X: el.X + el.W, Y: el.Y + el.H}}
		el.W, el.H = 0, 0
	default:
		draw.NormalizeShape(&el)
		if el.W < 2 && el.H < 2 {
			s.invalidate()
			return
		}
	}
	s.commit([]doc.Op{doc.AddOp(el)})
}

func dedupe(pts []doc.Point) []doc.Point {
	out := []doc.Point{pts[0]}
	for _, p := range pts[1:] {
		last := out[len(out)-1]
		if dx, dy := p.X-last.X, p.Y-last.Y; dx*dx+dy*dy >= 2.25 {
			out = append(out, p)
		}
	}
	if len(out) == 1 {
		return nil
	}
	return append(out, pts[len(pts)-1])
}

func copyEl(el doc.Element) doc.Element {
	c := el
	if el.Points != nil {
		c.Points = append([]doc.Point(nil), el.Points...)
	}
	return c
}

// ---------- history / ops ----------

func (s *BoardSession) commit(ops []doc.Op) {
	s.doc.Commit(ops)
	s.sendOps(ops)
	s.invalidate()
}

func (s *BoardSession) sendOps(ops []doc.Op) {
	if s.client != nil && len(ops) > 0 {
		s.client.Send(netx.Message{Type: "ops", Ops: ops})
	}
}

func (s *BoardSession) undo() {
	if ops := s.doc.Undo(); ops != nil {
		s.sendOps(ops)
		s.invalidate()
	}
}

func (s *BoardSession) redo() {
	if ops := s.doc.Redo(); ops != nil {
		s.sendOps(ops)
		s.invalidate()
	}
}

func (s *BoardSession) onRemote(msg netx.Message) {
	glib.IdleAdd(func() {
		if s.closed {
			return
		}
		switch msg.Type {
		case "sync":
			s.doc.ReplaceAll(msg.Elements)
			s.invalidate()
		case "ops":
			s.doc.Apply(msg.Ops)
			s.invalidate()
		}
	})
}

func (s *BoardSession) onPalette(p *palette.Palette) {
	if s.app.cfg.Color == "" && s.colorBtn != nil {
		s.color = p.FG
		s.colorBtn.SetRGBA(rgbaOf(p.FG))
	}
	if s.hub != nil {
		s.hub.SetPalette(p)
	}
	s.invalidate()
}

// ---------- keyboard ----------

func (s *BoardSession) onKey(keyval uint, state gdk.ModifierType) bool {
	r := unicode.ToLower(rune(keyval))
	if state&gdk.ControlMask != 0 {
		switch r {
		case 'z':
			if state&gdk.ShiftMask != 0 {
				s.redo()
			} else {
				s.undo()
			}
			return true
		case 'y':
			s.redo()
			return true
		}
		return false
	}
	if r > unicode.MaxASCII {
		return false
	}
	switch r {
	case 'v':
		s.setTool("select")
	case 'h':
		s.setTool("hand")
	case 'p':
		s.setTool("pen")
	case 'l':
		s.setTool("line")
	case 'r':
		s.setTool("rect")
	case 'o':
		s.setTool("ellipse")
	case 't':
		s.setTool("text")
	case 'e':
		s.setTool("eraser")
	default:
		return false
	}
	return true
}

// ---------- dialogs / files ----------

func (s *BoardSession) textDialog(x, y float64) {
	d := gtk.NewDialog()
	d.SetTransientFor(&s.app.win.Window)
	d.SetModal(true)
	d.SetTitle("Текст")
	d.AddButton("Отмена", int(gtk.ResponseCancel))
	d.AddButton("Добавить", int(gtk.ResponseOK))

	entry := gtk.NewEntry()
	entry.SetPlaceholderText("Введите текст")
	entry.SetHExpand(true)
	area := d.ContentArea()
	area.SetSpacing(8)
	area.SetMarginTop(12)
	area.SetMarginBottom(12)
	area.SetMarginStart(12)
	area.SetMarginEnd(12)
	area.Append(entry)

	d.ConnectResponse(func(resp int) {
		if resp == int(gtk.ResponseOK) {
			if txt := entry.Text(); txt != "" {
				s.commit([]doc.Op{doc.AddOp(doc.Element{
					ID: uuid.NewString(), Type: "text",
					Color: s.color, StrokeW: s.strokeW,
					X: x, Y: y, Text: txt,
				})})
			}
		}
		d.Close()
	})
	d.Present()
	entry.GrabFocus()
}

func (s *BoardSession) exportPNG() {
	nc := gtk.NewFileChooserNative(
		"Сохранить PNG", &s.app.win.Window,
		gtk.FileChooserActionSave, "Сохранить", "Отмена",
	)
	nc.SetCurrentName(fmt.Sprintf("omaboard-%d.png", time.Now().Unix()))
	nc.ConnectResponse(func(resp int) {
		defer nc.Hide()
		if resp != int(gtk.ResponseAccept) {
			return
		}
		f := nc.File()
		if f == nil {
			return
		}
		path := f.Path()
		if path == "" {
			return
		}
		els := s.doc.Elements()
		var x0, y0, x1, y1 float64
		if len(els) == 0 {
			x0, y0 = s.panX, s.panY
			w, h := s.width, s.height
			if w <= 0 {
				w = 900
			}
			if h <= 0 {
				h = 600
			}
			x1, y1 = x0+float64(w), y0+float64(h)
		} else {
			x0, y0, x1, y1 = draw.Bounds(els[0])
			for _, el := range els[1:] {
				bx, by, bw, bh := draw.Bounds(el)
				if bx < x0 {
					x0 = bx
				}
				if by < y0 {
					y0 = by
				}
				if bx+bw > x1 {
					x1 = bx + bw
				}
				if by+bh > y1 {
					y1 = by + bh
				}
			}
			const pad = 40.0
			x0, y0 = x0-pad, y0-pad
			x1, y1 = x1+pad, y1+pad
		}
		w := int(math.Ceil(x1 - x0))
		h := int(math.Ceil(y1 - y0))
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		if err := draw.ExportPNGRegion(path, els, s.app.pal, x0, y0, w, h); err != nil {
			s.setStatus(fmt.Sprintf("Ошибка экспорта: %v", err))
			return
		}
		s.setStatus("PNG сохранён: " + path)
	})
	nc.Show()
}

func (s *BoardSession) saveJSON() {
	nc := gtk.NewFileChooserNative(
		"Сохранить доску", &s.app.win.Window,
		gtk.FileChooserActionSave, "Сохранить", "Отмена",
	)
	nc.SetCurrentName(fmt.Sprintf("omaboard-%d.json", time.Now().Unix()))
	nc.ConnectResponse(func(resp int) {
		defer nc.Hide()
		if resp != int(gtk.ResponseAccept) {
			return
		}
		f := nc.File()
		if f == nil {
			return
		}
		if path := f.Path(); path != "" {
			if err := s.doc.Save(path); err != nil {
				s.setStatus(fmt.Sprintf("Ошибка сохранения: %v", err))
				return
			}
			s.setStatus("Доска сохранена: " + path)
		}
	})
	nc.Show()
}

func (s *BoardSession) loadJSON() {
	nc := gtk.NewFileChooserNative(
		"Открыть доску", &s.app.win.Window,
		gtk.FileChooserActionOpen, "Открыть", "Отмена",
	)
	nc.ConnectResponse(func(resp int) {
		defer nc.Hide()
		if resp != int(gtk.ResponseAccept) {
			return
		}
		f := nc.File()
		if f == nil {
			return
		}
		path := f.Path()
		if path == "" {
			return
		}
		els, err := doc.LoadFile(path)
		if err != nil {
			s.setStatus(fmt.Sprintf("Ошибка открытия: %v", err))
			return
		}
		old := s.doc.ReplaceAll(els)
		ops := make([]doc.Op, 0, len(old)+len(els))
		for _, el := range old {
			ops = append(ops, doc.DelOp(el))
		}
		for _, el := range els {
			ops = append(ops, doc.AddOp(el))
		}
		s.sendOps(ops)
		s.selected = ""
		s.invalidate()
		s.setStatus("Доска открыта: " + path)
	})
	nc.Show()
}

func (s *BoardSession) clear() {
	ops := s.doc.ClearOps()
	if ops == nil {
		return
	}
	s.selected = ""
	s.doc.Commit(ops)
	s.sendOps(ops)
	s.invalidate()
}

// ---------- lifecycle ----------

func (s *BoardSession) teardown() {
	s.closed = true
	if s.client != nil {
		s.client.Close()
	}
	if s.hub != nil {
		s.hub.Close()
	}
	if s.keyCtl != nil {
		s.app.win.RemoveController(s.keyCtl)
	}
}

// ---------- helpers ----------

func rgbaOf(hex string) *gdk.RGBA {
	r, g, b := palette.HexRGB(hex)
	rgba := gdk.NewRGBA(float32(r), float32(g), float32(b), 1)
	return &rgba
}

func hexOf(c *gdk.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x",
		int(float64(c.Red())*255+0.5),
		int(float64(c.Green())*255+0.5),
		int(float64(c.Blue())*255+0.5))
}

// showError shows a modal error dialog.
func (a *App) showError(title, text string) {
	d := gtk.NewDialog()
	d.SetTransientFor(&a.win.Window)
	d.SetModal(true)
	d.SetTitle(title)
	d.AddButton("ОК", int(gtk.ResponseOK))

	l := gtk.NewLabel(text)
	l.SetWrap(true)
	l.SetMaxWidthChars(56)
	l.SetMarginTop(14)
	l.SetMarginBottom(14)
	l.SetMarginStart(16)
	l.SetMarginEnd(16)
	d.ContentArea().Append(l)

	d.ConnectResponse(func(int) { d.Close() })
	d.Present()
}
