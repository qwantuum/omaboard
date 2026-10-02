package ui

import (
	"testing"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"omaboard/internal/doc"
	"omaboard/internal/palette"
)

// newTestSession builds a session with no GTK widgets attached, so the
// pointer logic can be exercised headlessly.
func newTestSession(tool string) *BoardSession {
	return &BoardSession{
		app:     &App{pal: palette.Nord(), cfg: Config{Stroke: 3}},
		mode:    "local",
		doc:     doc.New(),
		tool:    tool,
		color:   "#d8dee9",
		strokeW: 3,
		toggles: map[string]*gtk.ToggleButton{},
	}
}

func TestPenDrawCreatesElement(t *testing.T) {
	s := newTestSession("pen")
	s.onPress(10, 10)
	s.onMotion(50, 40)
	s.onMotion(90, 20)
	s.onRelease()
	if s.doc.Len() != 1 {
		t.Fatalf("expected 1 element, got %d", s.doc.Len())
	}
	el := s.doc.Elements()[0]
	if el.Type != "pen" || len(el.Points) < 3 {
		t.Fatalf("bad pen element: %+v", el)
	}
}

func TestRectDrawCreatesElement(t *testing.T) {
	s := newTestSession("rect")
	s.onPress(20, 20)
	s.onMotion(120, 80)
	s.onRelease()
	if s.doc.Len() != 1 {
		t.Fatalf("expected 1 element, got %d", s.doc.Len())
	}
	el := s.doc.Elements()[0]
	if el.Type != "rect" || el.W != 100 || el.H != 60 {
		t.Fatalf("bad rect: %+v", el)
	}
}

func TestClickWithoutDragIsDiscarded(t *testing.T) {
	s := newTestSession("rect")
	s.onPress(20, 20)
	s.onRelease()
	if s.doc.Len() != 0 {
		t.Fatalf("tiny shapes must be discarded, got %d", s.doc.Len())
	}
}

func TestUndoRedo(t *testing.T) {
	s := newTestSession("pen")
	s.onPress(0, 0)
	s.onMotion(30, 30)
	s.onRelease()
	if s.doc.Len() != 1 {
		t.Fatalf("setup failed: %d", s.doc.Len())
	}
	s.undo()
	if s.doc.Len() != 0 {
		t.Fatalf("undo failed: %d", s.doc.Len())
	}
	s.redo()
	if s.doc.Len() != 1 {
		t.Fatalf("redo failed: %d", s.doc.Len())
	}
}

func TestEraserRemovesHitElement(t *testing.T) {
	s := newTestSession("rect")
	s.onPress(20, 20)
	s.onMotion(120, 80)
	s.onRelease()

	s.tool = "eraser"
	s.onPress(60, 40) // inside the rect
	if s.doc.Len() != 0 {
		t.Fatalf("eraser failed: %d left", s.doc.Len())
	}
	s.undo()
	if s.doc.Len() != 1 {
		t.Fatalf("undo of erase failed: %d", s.doc.Len())
	}
}

func TestSelectAndMoveCommitsUpdate(t *testing.T) {
	s := newTestSession("rect")
	s.onPress(20, 20)
	s.onMotion(120, 80)
	s.onRelease()

	s.tool = "select"
	s.onPress(60, 40)
	if s.drag == nil {
		t.Fatal("expected element under cursor to be selected")
	}
	s.onMotion(70, 50)
	s.onRelease()
	el := s.doc.Elements()[0]
	if el.X != 30 || el.Y != 30 {
		t.Fatalf("move failed: %+v", el)
	}
	s.undo()
	el = s.doc.Elements()[0]
	if el.X != 20 || el.Y != 20 {
		t.Fatalf("undo of move failed: %+v", el)
	}
}

func TestPanOffsetsWorldCoordinates(t *testing.T) {
	s := newTestSession("rect")
	s.panX, s.panY = 100, 50
	s.onPress(20, 20)
	s.onMotion(120, 80)
	s.onRelease()
	el := s.doc.Elements()[0]
	if el.X != 120 || el.Y != 70 || el.W != 100 || el.H != 60 {
		t.Fatalf("pan not applied: %+v", el)
	}
}

func TestHandToolPansView(t *testing.T) {
	s := newTestSession("hand")
	s.onPress(10, 10)
	s.onMotion(60, 40)
	if s.panX != -50 || s.panY != -30 {
		t.Fatalf("pan drag failed: got %v,%v", s.panX, s.panY)
	}
	s.onRelease()
	if s.panning {
		t.Fatal("panning must end on release")
	}
}

func TestScrollPansView(t *testing.T) {
	s := newTestSession("pen")
	s.onScroll(5, -8)
	if s.panX != 5 || s.panY != -8 {
		t.Fatalf("scroll pan failed: %v,%v", s.panX, s.panY)
	}
	s.current = &doc.Element{}
	s.onScroll(100, 100)
	if s.panX != 5 || s.panY != -8 {
		t.Fatalf("scroll must be ignored while drawing: %v,%v", s.panX, s.panY)
	}
}

func TestURLToWS(t *testing.T) {
	cases := map[string]string{
		"http://192.168.1.5:8090":       "ws://192.168.1.5:8090/ws",
		"https://abc.trycloudflare.com": "wss://abc.trycloudflare.com/ws",
		"ws://host:1/ws":                "ws://host:1/ws",
		"https://host":                  "wss://host/ws",
	}
	for in, want := range cases {
		if got := urlToWS(in); got != want {
			t.Errorf("urlToWS(%q) = %q, want %q", in, got, want)
		}
	}
}
