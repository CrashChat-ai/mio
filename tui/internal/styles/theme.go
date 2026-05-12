// Package styles centralises lipgloss styles so the views render with a
// consistent palette. Kept minimal — extending the theme means adding
// here, not inline in views.
package styles

import "github.com/charmbracelet/lipgloss"

// Title is the top-of-pane heading.
var Title = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("39")). // cyan-ish
	Padding(0, 1)

// Subtle is for secondary text (footer hints, timestamps).
var Subtle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

// SelectedRow highlights the active row in lists.
var SelectedRow = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("0")).
	Background(lipgloss.Color("39"))

// TableHeader renders the lipgloss capability matrix header.
var TableHeader = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("245")).
	BorderStyle(lipgloss.NormalBorder()).
	BorderBottom(true).
	BorderForeground(lipgloss.Color("241"))

// TableCell is the default cell style.
var TableCell = lipgloss.NewStyle().Padding(0, 1)
