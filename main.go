package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godynamo/internal/app"
	"github.com/godynamo/internal/gui"
)

func main() {
	// `godynamo gui` launches the Electron desktop UI; everything else runs the TUI.
	if len(os.Args) > 1 && os.Args[1] == "gui" {
		if err := gui.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error running GoDynamo GUI: %v\n", err)
			os.Exit(1)
		}
		return
	}

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

