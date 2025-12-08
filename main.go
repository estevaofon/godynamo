package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godynamo/internal/app"
	"github.com/godynamo/internal/console"
)

func init() {
	// Set UTF-8 code page on Windows for proper unicode support
	_ = console.SetupUTF8()
	_ = console.EnableVirtualTerminal()
}

func main() {
	// Create the application model
	model := app.New()

	// Create the Bubble Tea program
	// Note: Mouse capture is disabled to allow text selection in terminal
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
	)

	// Run the program
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running GoDynamo: %v\n", err)
		os.Exit(1)
	}
}

