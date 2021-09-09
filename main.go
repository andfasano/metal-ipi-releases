package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

func OpenLinkInBrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Fatal(err)
	}

}

type Entry struct {
	Name string

	// Links

	Dashboard string
	Artifacts string
	Sippy     string
}

type Styles struct {
	Regular     ui.Style
	Highlighted ui.Style
}

type State struct {
	Table      *widgets.Table
	Styles     Styles
	Entries    []Entry
	Cursor     int
	LinkCursor int
}

func NewStyles() Styles {
	return Styles{
		Regular:     ui.NewStyle(ui.ColorWhite, ui.ColorBlack),
		Highlighted: ui.NewStyle(ui.ColorWhite, ui.ColorBlue),
	}
}

func NewState() State {
	w, h := ui.TerminalDimensions()
	table := widgets.NewTable()
	table.Title = "metal-ipi-releases"
	table.TextStyle = ui.NewStyle(ui.ColorWhite)
	table.RowSeparator = true
	table.BorderStyle = ui.NewStyle(ui.ColorGreen)
	table.SetRect(0, 0, w, h)
	table.FillRow = true

	return State{
		Table:      table,
		Styles:     NewStyles(),
		Cursor:     1,
		LinkCursor: 0,
	}
}

func GetSelectedLink(state State) string {
	entry := state.Entries[state.Cursor-1]
	switch state.LinkCursor {
	case 0:
		return entry.Dashboard
	case 1:
		return entry.Artifacts
	case 2:
		return entry.Sippy
	}
	return ""
}

func Redraw(state State) {
	rows := [][]string{
		{"Name", "Links"},
	}
	for i, entry := range state.Entries {
		links := ""
		values := []string{
			"dashboard",
			"artifacts",
			"sippy",
		}
		if i+1 == state.Cursor {

			for j, value := range values {
				if j == state.LinkCursor {
					links += fmt.Sprintf("[%s](fg:white,bg:green)", value)
				} else {
					links += value
				}

				links += " "
			}

			rows = append(rows, []string{entry.Name, links})
		} else {
			rows = append(rows, []string{entry.Name, strings.Join(values, " ")})
		}
	}

	state.Table.Rows = rows

	for i := range rows {
		if i == state.Cursor {
			state.Table.RowStyles[i] = state.Styles.Highlighted
		} else {
			state.Table.RowStyles[i] = state.Styles.Regular
		}
	}

	ui.Render(state.Table)
}

func main() {

	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	entries := []Entry{
		{Name: "4.10", Dashboard: "https://github.com/honza", Artifacts: "4.10 artifacts", Sippy: "4.10 sippy"},
		{Name: "4.9", Dashboard: "4.9 dash", Artifacts: "4.9 artifacts", Sippy: "4.9 sippy"},
	}

	state := NewState()
	state.Entries = entries

	Redraw(state)

	uiEvents := ui.PollEvents()
	for {
		e := <-uiEvents
		switch e.ID {
		case "j", "<Down>":
			state.Cursor++
			if state.Cursor > len(state.Entries) {
				state.Cursor = len(state.Entries)
			}
			Redraw(state)
		case "k", "<Up>":
			state.Cursor--
			if state.Cursor < 2 {
				state.Cursor = 1
			}
			Redraw(state)
		case "<Tab>":
			state.LinkCursor++
			if state.LinkCursor > 2 {
				state.LinkCursor = 0
			}
			Redraw(state)
		case "<Enter>":
			// Open url
			link := GetSelectedLink(state)
			OpenLinkInBrowser(link)
		case "q", "<C-c>":
			return
		}
	}
}
