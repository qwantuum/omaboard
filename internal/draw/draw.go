package draw

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"

	"omaboard/internal/doc"
	"omaboard/internal/palette"
)

// Background fills the whole canvas with the palette background color.
func Background(cr *cairo.Context, p *palette.Palette, w, h float64) {
	r, g, b := palette.HexRGB(p.Canvas)
	cr.SetSourceRGBA(r, g, b, 1)
	cr.Rectangle(0, 0, w, h)
	cr.Fill()
}

// Grid draws a subtle dot grid to give spatial reference. The grid is
// anchored to world coordinates: panX/panY is the world point at the view's
// top-left corner and w/h is the visible window size.
func Grid(cr *cairo.Context, p *palette.Palette, panX, panY, w, h float64) {
	r, g, b := palette.HexRGB(p.Border)
	cr.SetSourceRGBA(r, g, b, 0.6)
	cr.SetLineWidth(1)
	const step = 28.0
	x0 := math.Floor(panX/step) * step
	y0 := math.Floor(panY/step) * step
	for x := x0; x < panX+w; x += step {
		for y := y0; y < panY+h; y += step {
			cr.Arc(x, y, 1, 0, 2*math.Pi)
			cr.Fill()
		}
	}
}

// Elements paints the whole document.
func Elements(cr *cairo.Context, els []doc.Element) {
	for _, el := range els {
		Element(cr, el)
	}
}

// Element paints a single object.
func Element(cr *cairo.Context, el doc.Element) {
	r, g, b := palette.HexRGB(el.Color)
	cr.SetSourceRGBA(r, g, b, 1)
	cr.SetLineWidth(strokeW(el))
	cr.SetLineCap(cairo.LineCapRound)
	cr.SetLineJoin(cairo.LineJoinRound)

	switch el.Type {
	case "pen":
		if len(el.Points) < 2 {
			return
		}
		cr.MoveTo(el.Points[0].X, el.Points[0].Y)
		for _, p := range el.Points[1:] {
			cr.LineTo(p.X, p.Y)
		}
		cr.Stroke()
	case "line":
		if len(el.Points) < 2 {
			return
		}
		cr.MoveTo(el.Points[0].X, el.Points[0].Y)
		cr.LineTo(el.Points[1].X, el.Points[1].Y)
		cr.Stroke()
	case "rect":
		x, y, w, h := normalize(el)
		cr.Rectangle(x, y, w, h)
		cr.Stroke()
	case "ellipse":
		x, y, w, h := normalize(el)
		cx, cy := x+w/2, y+h/2
		rx, ry := math.Abs(w/2), math.Abs(h/2)
		if rx < 0.5 || ry < 0.5 {
			return
		}
		cr.Save()
		cr.Translate(cx, cy)
		cr.Scale(rx, ry)
		cr.Arc(0, 0, 1, 0, 2*math.Pi)
		cr.Restore()
		cr.Stroke()
	case "text":
		size := el.StrokeW*6 + 10
		cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightNormal)
		cr.SetFontSize(size)
		cr.MoveTo(el.X, el.Y)
		cr.ShowText(el.Text)
	}
}

// Selection draws an accent outline around the element's bounds.
func Selection(cr *cairo.Context, el doc.Element, p *palette.Palette) {
	x, y, w, h := Bounds(el)
	r, g, b := palette.HexRGB(p.Accent)
	cr.SetSourceRGBA(r, g, b, 1)
	cr.SetLineWidth(1.5)
	cr.SetDash([]float64{5, 4}, 0)
	cr.Rectangle(x-4, y-4, w+8, h+8)
	cr.Stroke()
	cr.SetDash(nil, 0)
}

// Bounds returns the axis-aligned bounding box of an element.
func Bounds(el doc.Element) (x, y, w, h float64) {
	switch el.Type {
	case "rect", "ellipse":
		return normalize(el)
	case "text":
		size := el.StrokeW*6 + 10
		est := float64(len([]rune(el.Text))) * size * 0.55
		return el.X, el.Y - size*0.8, est, size
	default: // pen, line
		if len(el.Points) == 0 {
			return el.X, el.Y, 0, 0
		}
		minX, minY := el.Points[0].X, el.Points[0].Y
		maxX, maxY := minX, minY
		for _, p := range el.Points[1:] {
			minX = math.Min(minX, p.X)
			minY = math.Min(minY, p.Y)
			maxX = math.Max(maxX, p.X)
			maxY = math.Max(maxY, p.Y)
		}
		return minX, minY, maxX - minX, maxY - minY
	}
}

// HitTest returns the topmost element under the point.
func HitTest(els []doc.Element, x, y float64) (doc.Element, bool) {
	for i := len(els) - 1; i >= 0; i-- {
		if hits(els[i], x, y) {
			return els[i], true
		}
	}
	return doc.Element{}, false
}

func hits(el doc.Element, x, y float64) bool {
	tol := math.Max(el.StrokeW/2+3, 5)
	switch el.Type {
	case "rect", "ellipse":
		bx, by, bw, bh := normalize(el)
		return x >= bx-tol && x <= bx+bw+tol && y >= by-tol && y <= by+bh+tol
	case "text":
		bx, by, bw, bh := Bounds(el)
		return x >= bx-tol && x <= bx+bw+tol && y >= by-tol && y <= by+bh+tol
	case "line":
		if len(el.Points) < 2 {
			return false
		}
		return distToSeg(x, y, el.Points[0], el.Points[1]) <= tol
	default: // pen
		if len(el.Points) == 1 {
			return math.Hypot(el.Points[0].X-x, el.Points[0].Y-y) <= tol
		}
		for i := 0; i < len(el.Points)-1; i++ {
			if distToSeg(x, y, el.Points[i], el.Points[i+1]) <= tol {
				return true
			}
		}
		return false
	}
}

func distToSeg(px, py float64, a, b doc.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	len2 := dx*dx + dy*dy
	if len2 == 0 {
		return math.Hypot(px-a.X, py-a.Y)
	}
	t := ((px-a.X)*dx + (py-a.Y)*dy) / len2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(a.X+t*dx), py-(a.Y+t*dy))
}

// NormalizeShape rewrites W/H so both are positive (finalized on pointer-up).
func NormalizeShape(el *doc.Element) {
	x, y, w, h := normalize(*el)
	el.X, el.Y, el.W, el.H = x, y, w, h
}

// Move translates the element by dx/dy.
func Move(el *doc.Element, dx, dy float64) {
	if el.Type == "pen" || el.Type == "line" {
		for i := range el.Points {
			el.Points[i].X += dx
			el.Points[i].Y += dy
		}
	}
	el.X += dx
	el.Y += dy
}

func normalize(el doc.Element) (x, y, w, h float64) {
	x, y = el.X, el.Y
	w, h = el.W, el.H
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	return x, y, w, h
}

func strokeW(el doc.Element) float64 {
	if el.StrokeW <= 0 {
		return 2
	}
	return el.StrokeW
}

// ExportPNG renders the document to a PNG file.
func ExportPNG(path string, els []doc.Element, p *palette.Palette, w, h int) error {
	return ExportPNGRegion(path, els, p, 0, 0, w, h)
}

// ExportPNGRegion renders the world rectangle whose top-left corner is
// (x, y) into a w*h image.
func ExportPNGRegion(path string, els []doc.Element, p *palette.Palette, x, y float64, w, h int) error {
	surface := cairo.CreateImageSurface(cairo.FormatARGB32, w, h)
	cr := cairo.Create(surface)
	Background(cr, p, float64(w), float64(h))
	cr.Save()
	cr.Translate(-x, -y)
	Elements(cr, els)
	cr.Restore()
	cr.Close()
	return surface.WriteToPNG(path)
}
