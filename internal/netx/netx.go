package netx

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"omaboard/internal/doc"
	"omaboard/internal/palette"
)

//go:embed web/*
var webFS embed.FS

// Message is the wire protocol shared by GTK and browser clients.
type Message struct {
	Type     string           `json:"type"` // sync, ops, theme
	Ops      []doc.Op         `json:"ops,omitempty"`
	Elements []doc.Element    `json:"elements,omitempty"`
	Palette  *palette.Palette `json:"palette,omitempty"`
}

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
	doc     *doc.Document
	bc      chan Message
	pal     *palette.Palette
	srv     *http.Server
	closed  bool
}

func NewHub(p *palette.Palette) *Hub {
	h := &Hub{
		clients: make(map[*websocket.Conn]struct{}),
		doc:     doc.New(),
		bc:      make(chan Message, 128),
		pal:     p,
	}
	go h.run()
	return h
}

// Serve starts the HTTP+WebSocket server on port and returns the actual URL.
func (h *Hub) Serve(port int) (string, error) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.handleWS)
	mux.Handle("/", http.FileServer(http.FS(sub)))

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return "", err
	}
	h.srv = &http.Server{Handler: mux}
	go func() {
		// Serve (not ListenAndServe) — otherwise the server would ignore ln
		// and try to bind port 80, leaving every handshake hanging forever.
		if err := h.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("omaboard: http server: %v", err)
		}
	}()
	return fmt.Sprintf("http://%s", ln.Addr().String()), nil
}

func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.bc)
	for c := range h.clients {
		c.Close()
	}
	h.clients = map[*websocket.Conn]struct{}{}
	h.mu.Unlock()
	if h.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		h.srv.Shutdown(ctx)
	}
}

// send queues a broadcast; callers must not hold h.mu.
func (h *Hub) send(msg Message) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	select {
	case h.bc <- msg:
		return true
	default:
		return false
	}
}

func (h *Hub) SetPalette(p *palette.Palette) {
	h.mu.Lock()
	h.pal = p
	h.mu.Unlock()
	h.send(Message{Type: "theme", Palette: p})
}

func (h *Hub) run() {
	for msg := range h.bc {
		h.mu.Lock()
		if msg.Type == "ops" {
			for i := range msg.Ops {
				if msg.Ops[i].Kind == doc.OpAdd && msg.Ops[i].El.ID == "" {
					msg.Ops[i].El.ID = uuid.NewString()
				}
			}
			h.doc.Apply(msg.Ops)
		}
		data, err := json.Marshal(msg)
		if err == nil {
			for c := range h.clients {
				if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
					c.Close()
					delete(h.clients, c)
				}
			}
		}
		h.mu.Unlock()
	}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	els := h.doc.Elements()
	pal := h.pal
	h.mu.Unlock()

	sync, _ := json.Marshal(Message{Type: "sync", Elements: els, Palette: pal})
	if err := conn.WriteMessage(websocket.TextMessage, sync); err != nil {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		return
	}

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			return
		}
		if msg.Type == "ops" && len(msg.Ops) > 0 {
			h.send(msg) // drop on overload; boards are small so this is rare
		}
	}
}

// Client is the GTK-side WebSocket connection.
type Client struct {
	conn *websocket.Conn
	send chan Message
	once sync.Once
}

// Dial opens a WebSocket connection with a handshake timeout so a bad network
// can never block the caller indefinitely.
func Dial(url string, onMsg func(Message)) (*Client, error) {
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, send: make(chan Message, 64)}
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			onMsg(msg)
		}
	}()
	go func() {
		for msg := range c.send {
			if err := c.conn.WriteJSON(msg); err != nil {
				return
			}
		}
	}()
	return c, nil
}

func (c *Client) Send(msg Message) {
	select {
	case c.send <- msg:
	default:
	}
}

func (c *Client) Close() {
	c.once.Do(func() {
		close(c.send)
		c.conn.Close()
	})
}

// LANIP returns the first non-loopback IPv4 address for the share URL.
func LANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
				return ipn.IP.String()
			}
		}
	}
	return ""
}
