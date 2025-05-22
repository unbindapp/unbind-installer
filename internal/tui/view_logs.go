package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Add debug view
func viewDebugLogs(m Model) string {
	s := strings.Builder{}
	s.WriteString(m.styles.Title.Render("Debug Logs"))
	s.WriteString("\n\n")

	if len(m.logMessages) == 0 {
		s.WriteString("No logs available")
	} else {
		for i, msg := range m.logMessages {
			s.WriteString(fmt.Sprintf("%d: %s\n", i, msg))
		}
	}

	s.WriteString("\n")
	s.WriteString(m.styles.StatusBar.Render("Press 'Ctrl+D' to return, 'Ctrl+c' to quit"))
	return s.String()
}

func (m Model) updateDebugLogsState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, m.listenForLogs()
}
