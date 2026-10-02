package draw

import (
	"os"
	"path/filepath"
	"testing"

	"omaboard/internal/doc"
	"omaboard/internal/palette"
)

func sampleElements() []doc.Element {
	return []doc.Element{
		{ID: "1", Type: "pen", Color: "#ea6962", StrokeW: 3,
			Points: []doc.Point{{X: 10, Y: 10}, {X: 100, Y: 60}, {X: 200, Y: 30}}},
		{ID: "2", Type: "rect", Color: "#a9b665", StrokeW: 4, X: 40, Y: 80, W: 150, H: 90},
		{ID: "3", Type: "ellipse", Color: "#7daea3", StrokeW: 2, X: 250, Y: 100, W: 120, H: 70},
		{ID: "4", Type: "line", Color: "#d3869b", StrokeW: 5,
			Points: []doc.Point{{X: 0, Y: 200}, {X: 300, Y: 260}}},
		{ID: "5", Type: "text", Color: "#d4be98", StrokeW: 3, X: 60, Y: 300, Text: "Привет, Omaboard"},
	}
}

func TestExportPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.png")
	if err := ExportPNG(path, sampleElements(), palette.Nord(), 800, 600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 1000 {
		t.Fatalf("suspiciously small PNG: %d bytes", st.Size())
	}
}

func TestHitTest(t *testing.T) {
	els := sampleElements()
	if _, ok := HitTest(els, 110, 120); !ok { // inside rect 40,80 150x90
		t.Fatal("expected rect hit")
	}
	if _, ok := HitTest(els, 700, 500); ok {
		t.Fatal("expected miss in empty area")
	}
}

func TestNormalizeShape(t *testing.T) {
	el := doc.Element{Type: "rect", X: 100, Y: 100, W: -50, H: -30}
	NormalizeShape(&el)
	if el.X != 50 || el.Y != 70 || el.W != 50 || el.H != 30 {
		t.Fatalf("bad normalize: %+v", el)
	}
}
