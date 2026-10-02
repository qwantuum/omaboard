package netx

import (
	"testing"
	"time"

	"omaboard/internal/palette"
)

func TestServeDialDoesNotHang(t *testing.T) {
	hub := NewHub(palette.Nord())
	defer hub.Close()

	done := make(chan string, 1)
	go func() {
		if _, err := hub.Serve(18090); err != nil {
			done <- "serve error: " + err.Error()
			return
		}
		gotSync := make(chan Message, 1)
		c, err := Dial("ws://127.0.0.1:18090/ws", func(m Message) {
			if m.Type == "sync" {
				gotSync <- m
			}
		})
		if err != nil {
			done <- "dial error: " + err.Error()
			return
		}
		defer c.Close()
		select {
		case m := <-gotSync:
			done <- "ok sync palette=" + m.Palette.Slug
		case <-time.After(3 * time.Second):
			done <- "TIMEOUT waiting sync"
		}
	}()

	select {
	case msg := <-done:
		t.Log(msg)
		if msg != "ok sync palette=nord" {
			t.Fatal(msg)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("HANG: Serve/Dial blocked")
	}
}
