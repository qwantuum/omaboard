package main

import (
	"flag"
	"os"

	"omaboard/internal/ui"
)

func main() {
	connect := flag.String("connect", "",
		"подключиться к серверной доске (http://host:port или ws://host:port/ws)")
	port := flag.Int("port", 8090, "порт для серверной (мультиплеер) доски")
	server := flag.Bool("server", false, "сразу открыть серверную (мультиплеер) доску")
	flag.Parse()

	startMode := ""
	if *server {
		startMode = "server"
	}
	// GTK parses argv too — hand it only the program name and leftover
	// positional args, otherwise GApplication rejects our own flags.
	gtkArgs := append([]string{os.Args[0]}, flag.Args()...)
	os.Exit(ui.Run(gtkArgs, *connect, startMode, *port))
}
