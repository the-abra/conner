package main

import (
	"io"
	"log"
	"os"

	"conner/internal/config"
	"conner/internal/server"
	"conner/internal/server/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	srv := server.NewServer()

	// Start server goroutine before silencing logs so any startup errors
	// (port already in use, etc.) can still print to stderr.
	startErr := make(chan error, 1)
	go func() {
		startErr <- srv.Start(config.ServerPort)
	}()

	// Silence log.Println once the TUI takes over the terminal.
	// All log entries are stored in srv.ConsoleHistory and shown in the
	// Dashboard tab — raw stdout output would corrupt the TUI rendering.
	log.SetOutput(io.Discard)

	p := tea.NewProgram(tui.InitialModel(srv), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		// Restore stderr for the fatal message after TUI exits.
		log.SetOutput(os.Stderr)
		log.Fatal(err)
	}
}
