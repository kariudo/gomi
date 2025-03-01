package keys

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
)

type DetailKeyMap struct {
	Space  key.Binding
	Next   key.Binding
	Prev   key.Binding
	Esc    key.Binding
	Quit   key.Binding
	AtSign key.Binding
	Delete key.Binding

	// Preview
	GotoTop      key.Binding
	GotoBottom   key.Binding
	PreviewUp    key.Binding
	PreviewDown  key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding

	Help      key.Binding
	HelpClose key.Binding

	showDelete bool
}

func (k DetailKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Next,
		k.Prev,
		k.Space,
		k.Help,
	}
}

func (k DetailKeyMap) FullHelp() [][]key.Binding {
	first := []key.Binding{
		k.Next, k.Prev, k.Space, k.Esc, k.AtSign,
	}
	if k.showDelete {
		first = append(first, k.Delete)
	}
	return [][]key.Binding{
		first,
		{k.PreviewUp, k.PreviewDown, k.HalfPageUp, k.HalfPageDown, k.GotoTop, k.GotoBottom},
		{k.HelpClose, k.Quit},
	}
}

var DetailKeys = &DetailKeyMap{
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "more"),
	),
	HelpClose: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "close help"),
	),
	Space: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "back"),
	),
	PreviewUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "preview up"),
	),
	PreviewDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "preview down"),
	),
	Next: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next"),
	),
	Prev: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "prev"),
	),
	Esc: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	AtSign: key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "info"),
	),
	GotoTop:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "jump to top")),
	GotoBottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "jump to bottom")),
	HalfPageUp:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "half page up")),
	HalfPageDown: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "half page down")),
}

var PreviewKeys = viewport.KeyMap{
	Up:           key.NewBinding(key.WithKeys("k", "up")),
	Down:         key.NewBinding(key.WithKeys("j", "down")),
	HalfPageUp:   key.NewBinding(key.WithKeys("u")),
	HalfPageDown: key.NewBinding(key.WithKeys("d")),
}

func (k *DetailKeyMap) AddDeleteKey() {
	k.showDelete = true
	k.Delete = key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "delete"),
	)
}
