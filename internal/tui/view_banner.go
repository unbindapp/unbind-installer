package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/unbindapp/unbind-installer/config"
)

// getBanner returns a stylized ASCII art banner for the application
func getBanner() string {
	// ASCII art for "Unbind"
	asciiArt := []string{
		" _   _       _     _           _",
		"| | | |_ __ | |__ (_)_ __   __| |",
		"| | | | '_ \\| '_ \\| | '_ \\ / _` |",
		"| |_| | | | | |_) | | | | | (_| |",
		" \\___/|_| |_|_.__/|_|_| |_|\\__,_|",
	}

	// Create a bold, gradient-colored style for the banner
	baseStyle := lipgloss.NewStyle().Bold(true)

	// Create colors for a gradient effect - green theme
	colors := []string{"#005500", "#006600", "#007700", "#008800", "#009900"}

	// Apply gradient to ASCII art lines
	var styledLines []string
	for i, line := range asciiArt {
		colorIdx := i % len(colors)
		styledLine := baseStyle.Foreground(lipgloss.Color(colors[colorIdx])).Render(line)
		styledLines = append(styledLines, styledLine)
	}

	// Create subtitle with version
	versionText := fmt.Sprintf("Installer v%s", config.VERSION)
	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00cc00")).
		Bold(true).
		Italic(true)

	// Center the version text under the banner
	bannerWidth := len(asciiArt[0])
	paddingSize := (bannerWidth - len(versionText)) / 2
	if paddingSize < 0 {
		paddingSize = 0
	}
	padding := strings.Repeat(" ", paddingSize)

	styledVersion := padding + versionStyle.Render(versionText)

	// Combine the styled lines and version
	banner := strings.Join(styledLines, "\n") + "\n" + styledVersion

	return banner
}
