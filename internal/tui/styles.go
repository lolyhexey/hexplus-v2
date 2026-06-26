// Package tui owns the bubble-tea-driven menu HEXPLUS v2 ships as its
// interactive surface. v1 customers spent their day inside Modulos/conexao;
// this package recreates that Thai-labeled menu tree on top of the same
// internal/* packages the CLI subcommands already use.
package tui

import "github.com/charmbracelet/lipgloss"

// Color palette intentionally mirrors the ANSI codes Modulos/conexao
// used so the menu feels familiar to v1 operators.
var (
	colorRed    = lipgloss.Color("9")   // bright red - errors, warnings, off
	colorGreen  = lipgloss.Color("10")  // bright green - on, confirmations
	colorYellow = lipgloss.Color("11")  // bright yellow - labels, prompts
	colorBlue   = lipgloss.Color("12")  // bright blue - banners
	colorCyan   = lipgloss.Color("14")  // bright cyan - hostnames, values
	colorWhite  = lipgloss.Color("15")  // bright white - body text
	colorDim    = lipgloss.Color("8")   // dim gray - help footer
)

// styles is the curated set of lipgloss styles every view inside this
// package reaches for. Single source of truth so a theme tweak lands in
// one place.
var styles = struct {
	Title       lipgloss.Style
	Header      lipgloss.Style
	HeaderRule  lipgloss.Style
	ItemNormal  lipgloss.Style
	ItemActive  lipgloss.Style
	ItemDisable lipgloss.Style
	StatusOn    lipgloss.Style
	StatusOff   lipgloss.Style
	StatusLabel lipgloss.Style
	StatusValue lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style
	Footer      lipgloss.Style
	Error       lipgloss.Style
	Form        lipgloss.Style
	FormLabel   lipgloss.Style
	Banner      lipgloss.Style
}{
	Title: lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWhite).
		Background(colorBlue).
		Padding(0, 2),
	Header: lipgloss.NewStyle().
		Bold(true).
		Foreground(colorYellow),
	HeaderRule: lipgloss.NewStyle().
		Foreground(colorRed),
	ItemNormal: lipgloss.NewStyle().
		Foreground(colorWhite),
	ItemActive: lipgloss.NewStyle().
		Bold(true).
		Foreground(colorGreen),
	ItemDisable: lipgloss.NewStyle().
		Foreground(colorDim),
	StatusOn: lipgloss.NewStyle().
		Bold(true).
		Foreground(colorGreen),
	StatusOff: lipgloss.NewStyle().
		Foreground(colorRed),
	StatusLabel: lipgloss.NewStyle().
		Foreground(colorYellow),
	StatusValue: lipgloss.NewStyle().
		Foreground(colorCyan),
	HelpKey: lipgloss.NewStyle().
		Foreground(colorCyan).
		Bold(true),
	HelpDesc: lipgloss.NewStyle().
		Foreground(colorDim),
	Footer: lipgloss.NewStyle().
		Foreground(colorDim).
		MarginTop(1),
	Error: lipgloss.NewStyle().
		Foreground(colorRed).
		Bold(true),
	Form: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2),
	FormLabel: lipgloss.NewStyle().
		Foreground(colorYellow).
		Bold(true),
	Banner: lipgloss.NewStyle().
		Bold(true).
		Foreground(colorRed),
}

// runeOn / runeOff are the indicators v1 used (◉/○). Keeping the same
// glyphs means operators don't relearn what "active" looks like.
const (
	runeOn  = "●"
	runeOff = "○"
)
