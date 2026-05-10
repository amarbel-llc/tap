package style

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var renderer = func() *lipgloss.Renderer {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	return r
}()

var (
	Green  = renderer.NewStyle().Foreground(lipgloss.Color("2"))
	Red    = renderer.NewStyle().Foreground(lipgloss.Color("1"))
	Yellow = renderer.NewStyle().Foreground(lipgloss.Color("3"))
)
