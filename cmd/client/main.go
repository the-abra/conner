package main

import (
	"fmt"
	"log"
	"os"

	"conner/internal/client"
	"conner/internal/client/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/client/main.go <nickname> [address:port]")
		os.Exit(1)
	}
	nickname := os.Args[1]
	
	address := "127.0.0.1:6666" // default
	if len(os.Args) >= 3 {
		address = os.Args[2]
	}

	cli, err := client.Connect(nickname, address)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	p := tea.NewProgram(tui.InitialModel(cli, nickname), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
