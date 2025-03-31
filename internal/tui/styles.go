package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// AppTheme holds the color theme for the application
type AppTheme struct {
	Primary    string // Main brand color (vibrant green)
	Secondary  string // Complementary color
	Accent     string // Accent color for highlights
	Text       string // Default text color
	Subtle     string // Subtle text color
	Error      string // Error color
	Success    string // Success color
	Background string // Background color
	Surface    string // Surface color for UI elements
}

// DefaultTheme returns the default color theme
func DefaultTheme() AppTheme {
	return AppTheme{
		Primary:    "#009900", // hsl(120 100% 30%) - Vibrant green
		Secondary:  "#005500", // Darker green
		Accent:     "#00cc00", // Lighter green
		Text:       "#ffffff", // White text
		Subtle:     "#888888", // Gray text
		Error:      "#ff0000", // Red
		Success:    "#00ff00", // Bright green
		Background: "#121212", // Dark background
		Surface:    "#242424", // Slightly lighter surface
	}
}

// NewStyles initializes the application styles with the provided theme
func NewStyles(theme AppTheme) Styles {
	return Styles{
		Title: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Primary)).
			Bold(true).
			MarginLeft(1).
			MarginBottom(1),

		Normal: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Text)),

		Bold: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Text)).
			Bold(true),

		Subtle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Subtle)),

		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Error)),

		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color(theme.Primary)).
			Padding(0, 1),

		Key: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Accent)).
			Bold(true),

		SpinnerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Primary)),

		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Success)).
			Bold(true),

		HighlightButton: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.Text)).
			Background(lipgloss.Color(theme.Primary)).
			Padding(0, 2).
			Bold(true),
	}
}

// Styles contains the styling for the UI
type Styles struct {
	Title           lipgloss.Style
	Normal          lipgloss.Style
	Bold            lipgloss.Style
	Subtle          lipgloss.Style
	Error           lipgloss.Style
	StatusBar       lipgloss.Style
	Key             lipgloss.Style
	SpinnerStyle    lipgloss.Style
	Success         lipgloss.Style
	HighlightButton lipgloss.Style
}
