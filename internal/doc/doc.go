package doc

import (
	"encoding/json"
	"os"
	"sync"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Element is a single drawing object. The JSON shape is shared with the web
// client, so field names and types must stay compatible.
type Element struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"` // pen, line, rect, ellipse, text
	Color   string  `json:"color"`
	StrokeW float64 `json:"strokeW"`
	Points  []Point `json:"points,omitempty"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	Text    string  `json:"text,omitempty"`
}

// Op kinds.
const (
	OpAdd = "add"
	OpDel = "del"
	OpUpd = "upd"
)

// Op is an atomic mutation. Undo/redo and network sync both travel as []Op.
type Op struct {
	Kind string   `json:"kind"`
	El   Element  `json:"el"`
	Prev *Element `json:"prev,omitempty"` // previous value for upd
}

func AddOp(el Element) Op       { return Op{Kind: OpAdd, El: el} }
func DelOp(el Element) Op       { return Op{Kind: OpDel, El: el} }
func UpdOp(new, old Element) Op { return Op{Kind: OpUpd, El: new, Prev: &old} }

type Document struct {
	mu      sync.Mutex
	els     []Element
	undo    [][]Op
	redo    [][]Op
	history int
}

func New() *Document { return &Document{} }

func (d *Document) Elements() []Element {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Element, len(d.els))
	copy(out, d.els)
	return out
}

func (d *Document) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.els)
}

func (d *Document) Get(id string) (Element, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, el := range d.els {
		if el.ID == id {
			return el, true
		}
	}
	return Element{}, false
}

// Commit applies local ops and records them for undo.
func (d *Document) Commit(ops []Op) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.applyLocked(ops)
	d.undo = append(d.undo, ops)
	if len(d.undo) > 200 {
		d.undo = d.undo[1:]
	}
	d.redo = nil
}

// Apply applies remote ops without touching local history.
func (d *Document) Apply(ops []Op) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.applyLocked(ops)
}

// Undo reverts the last local commit and returns the inverse ops to broadcast.
func (d *Document) Undo() []Op {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.undo) == 0 {
		return nil
	}
	batch := d.undo[len(d.undo)-1]
	d.undo = d.undo[:len(d.undo)-1]
	inv := invert(batch)
	d.applyLocked(inv)
	d.redo = append(d.redo, batch)
	return inv
}

// Redo reapplies the last undone batch.
func (d *Document) Redo() []Op {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.redo) == 0 {
		return nil
	}
	batch := d.redo[len(d.redo)-1]
	d.redo = d.redo[:len(d.redo)-1]
	d.applyLocked(batch)
	d.undo = append(d.undo, batch)
	return batch
}

// ReplaceAll swaps the whole content (used by JSON load). Returns old
// elements so the caller can build broadcast ops.
func (d *Document) ReplaceAll(els []Element) []Element {
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.els
	d.els = make([]Element, len(els))
	copy(d.els, els)
	d.undo = nil
	d.redo = nil
	return old
}

// Clear removes every element, returning a commit-ready del ops batch.
func (d *Document) ClearOps() []Op {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.els) == 0 {
		return nil
	}
	ops := make([]Op, 0, len(d.els))
	for _, el := range d.els {
		ops = append(ops, DelOp(el))
	}
	return ops
}

func (d *Document) Save(path string) error {
	els := d.Elements()
	b, err := json.MarshalIndent(els, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func LoadFile(path string) ([]Element, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var els []Element
	if err := json.Unmarshal(b, &els); err != nil {
		return nil, err
	}
	return els, nil
}

func (d *Document) applyLocked(ops []Op) {
	for _, op := range ops {
		switch op.Kind {
		case OpAdd:
			found := false
			for i, el := range d.els {
				if el.ID == op.El.ID {
					d.els[i] = op.El
					found = true
					break
				}
			}
			if !found {
				d.els = append(d.els, op.El)
			}
		case OpDel:
			for i, el := range d.els {
				if el.ID == op.El.ID {
					d.els = append(d.els[:i], d.els[i+1:]...)
					break
				}
			}
		case OpUpd:
			idx := -1
			for i, el := range d.els {
				if el.ID == op.El.ID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				d.els[idx] = op.El
			} else {
				d.els = append(d.els, op.El)
			}
		}
	}
}

func invert(ops []Op) []Op {
	out := make([]Op, len(ops))
	for i, op := range ops {
		switch op.Kind {
		case OpAdd:
			out[i] = DelOp(op.El)
		case OpDel:
			out[i] = AddOp(op.El)
		case OpUpd:
			if op.Prev != nil {
				out[i] = UpdOp(*op.Prev, op.El)
			} else {
				out[i] = Op{Kind: OpUpd, El: op.El}
			}
		}
	}
	return out
}
