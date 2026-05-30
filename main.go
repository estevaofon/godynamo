package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godynamo/internal/app"
	"github.com/godynamo/internal/gui"
)

type mode int

const (
	modeGUI mode = iota
	modeTUI
)

// selectMode decides which interface to launch from the CLI args (os.Args[1:]).
// Default is the GUI; `tui` selects the terminal UI; `gui` is an accepted alias
// for the default and is stripped so trailing flags pass through to gui.Run.
func selectMode(args []string) (mode, []string) {
	if len(args) > 0 && args[0] == "tui" {
		return modeTUI, args[1:]
	}
	if len(args) > 0 && args[0] == "gui" {
		return modeGUI, args[1:]
	}
	return modeGUI, args
}

func main() {
	m, rest := selectMode(os.Args[1:])
	if m == modeTUI {
		runTUI()
		return
	}
	if err := gui.Run(rest); err != nil {
		fmt.Fprintf(os.Stderr, "Error running GoDynamo GUI: %v\n", err)
		os.Exit(1)
	}
}

// runTUI launches the Bubble Tea terminal UI (mouse capture stays off so text
// selection works in the terminal).
func runTUI() {
	model := app.New()
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running GoDynamo: %v\n", err)
		os.Exit(1)
	}
}
