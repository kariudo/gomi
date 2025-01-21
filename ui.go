package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/gabriel-vasile/mimetype"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/termenv"
	"github.com/samber/lo"
)

type NavState int

const (
	INVENTORY_LIST NavState = iota
	INVENTORY_DETAILS
	QUITTING
)

type DetailsMsg struct {
	file File
}
type GotInventorysMsg struct {
	files []list.Item
}

type errorMsg struct {
	err error
}

func (e errorMsg) Error() string { return e.err.Error() }

func errorCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return errorMsg{err}
	}
}

type model struct {
	navState   NavState
	detailFile File
	datefmt    string

	files   []File
	cli     *CLI
	choices []File

	list list.Model
	err  error
}

const (
	bullet   = "•"
	ellipsis = "…"
)

func (f File) Description() string {
	var meta []string
	meta = append(meta, humanize.Time(f.Timestamp))

	_, err := os.Stat(f.To)
	if os.IsNotExist(err) {
		return "(already might have been deleted)"
	}
	meta = append(meta, filepath.Dir(f.From))

	return strings.Join(meta, " "+bullet+" ")
}

func (f File) Title() string {
	fi, err := os.Stat(f.To)
	if err != nil {
		return f.Name + "?"
	}
	if fi.IsDir() {
		return f.Name + "/"
	}
	return f.Name
}

func (f File) FilterValue() string {
	return f.Name
}

var _ list.DefaultItem = (*File)(nil)

type inventoryLoadedMsg struct {
	files []list.Item
	err   error
}

func (m model) loadInventory() tea.Msg {
	// files := m.cli.inventory.Files
	files := m.files
	if len(files) == 0 {
		return errorMsg{errors.New("no deleted files found")}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Timestamp.After(files[j].Timestamp)
	})
	files = lo.Filter(files, func(file File, index int) bool {
		// filter not found inventory out
		_, err := os.Stat(file.To)
		if os.IsNotExist(err) {
			return false
		}
		return true
	})
	items := make([]list.Item, len(files))
	for i, file := range files {
		items[i] = file
	}
	return inventoryLoadedMsg{files: items}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.loadInventory,
	)
}

type (
	keyMap struct {
		Quit     key.Binding
		Select   key.Binding
		DeSelect key.Binding
	}
	listAdditionalKeyMap struct {
		Enter key.Binding
		Space key.Binding
	}
	detailKeyMap struct {
		Up     key.Binding
		Down   key.Binding
		Esc    key.Binding
		AtSign key.Binding
	}
)

var (
	keys = keyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Select: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "select"),
		),
		DeSelect: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("s+tab", "de-select"),
		),
	}
	listAdditionalKeys = listAdditionalKeyMap{
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "ok"),
		),
		Space: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "info"),
		),
	}
	detailKeys = detailKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Esc: key.NewBinding(
			key.WithKeys("esc"), // space itself should be already defined another part
			key.WithHelp("space/esc", "back"),
		),
		AtSign: key.NewBinding(
			key.WithKeys("@"), // space itself should be already defined another part
			key.WithHelp("@", "datefmt"),
		),
	}
)

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{detailKeys.Up, detailKeys.Down, detailKeys.Esc, detailKeys.AtSign}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp(), {}}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			m.navState = QUITTING
			return m, tea.Quit

		case key.Matches(msg, keys.Select):
			if m.list.FilterState() != list.Filtering {
				item, ok := m.list.SelectedItem().(File)
				if !ok {
					break
				}
				if item.isSelected() {
					selectionManager.Remove(item)
				} else {
					selectionManager.Add(item)
				}
				m.list.CursorDown()
				if m.navState == INVENTORY_DETAILS {
					cmds = append(cmds, getInventoryDetails(item))
				}
			}

		case key.Matches(msg, keys.DeSelect):
			if m.list.FilterState() != list.Filtering {
				item, ok := m.list.SelectedItem().(File)
				if !ok {
					break
				}
				if item.isSelected() {
					selectionManager.Remove(item)
				}
				m.list.CursorUp()
				if m.navState == INVENTORY_DETAILS {
					cmds = append(cmds, getInventoryDetails(item))
				}
			}

		case key.Matches(msg, detailKeys.AtSign):
			switch m.navState {
			case INVENTORY_DETAILS:
				switch m.datefmt {
				case datefmtRel:
					m.datefmt = datefmtAbs
				case datefmtAbs:
					m.datefmt = datefmtRel
				}
			}

		case key.Matches(msg, detailKeys.Up):
			switch m.navState {
			case INVENTORY_DETAILS:
				m.list.CursorUp()
				file, ok := m.list.SelectedItem().(File)
				if ok {
					cmds = append(cmds, getInventoryDetails(file))
				}
			}
		case key.Matches(msg, detailKeys.Down):
			switch m.navState {
			case INVENTORY_DETAILS:
				m.list.CursorDown()
				file, ok := m.list.SelectedItem().(File)
				if ok {
					cmds = append(cmds, getInventoryDetails(file))
				}
			}

		case key.Matches(msg, detailKeys.Esc):
			switch m.navState {
			// case INVENTORY_LIST:
			// 	m.navState = INVENTORY_DETAILS
			case INVENTORY_DETAILS:
				m.navState = INVENTORY_LIST
			}

		case key.Matches(msg, listAdditionalKeys.Space):
			switch m.navState {
			case INVENTORY_LIST:
				if m.list.FilterState() != list.Filtering {
					file, ok := m.list.SelectedItem().(File)
					if ok {
						cmds = append(cmds, getInventoryDetails(file))
					}
				}
			case INVENTORY_DETAILS:
				m.navState = INVENTORY_LIST
			}

		case key.Matches(msg, listAdditionalKeys.Enter):
			switch m.navState {
			case INVENTORY_LIST:
				if m.list.FilterState() != list.Filtering {
					files := selectionManager.items
					if len(files) == 0 {
						file, ok := m.list.SelectedItem().(File)
						if ok {
							m.choices = append(m.choices, file)
						}
					} else {
						m.choices = files
					}
					return m, tea.Quit
				}
			}
		}

	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)

	case inventoryLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		for _, file := range msg.files {
			m.files = append(m.files, file.(File))
		}
		m.list.SetItems(msg.files)

	case DetailsMsg:
		m.detailFile = msg.file
		m.navState = INVENTORY_DETAILS

	case errorMsg:
		m.navState = QUITTING
		m.err = msg
		return m, tea.Quit
	}

	var cmd tea.Cmd
	switch m.navState {
	case INVENTORY_LIST:
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

var (
	AccentColor             = lipgloss.ANSIColor(termenv.ANSIBlack)
	stylesSectionStyle      = lipgloss.NewStyle().BorderStyle(lipgloss.HiddenBorder()).BorderForeground(AccentColor).Padding(0, 1)
	stylesSectionTitleStyle = lipgloss.NewStyle().Padding(0, 1).MarginBottom(1).Background(AccentColor).Foreground(lipgloss.Color("15")).Bold(true).Transform(strings.ToUpper)
)

func renderInventoryDetails(m model) string {
	header := renderHeader(m.detailFile)

	content := lipgloss.JoinVertical(lipgloss.Left,
		header,
		renderDeletedWhere(m.detailFile),
		renderDeletedAt(m.detailFile, m.datefmt),
		renderMetadata(m.detailFile),
		strings.Repeat("─", lipgloss.Width(header)),
	)

	return content
}

func renderHeader(file File) string {
	name := file.Name
	if file.isSelected() {
		// name = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"}).Render(file.Name)
		name = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#000000"}).
			Background(lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"}).
			Render(file.Name)
	}
	title := lipgloss.NewStyle().
		BorderStyle(func() lipgloss.Border {
			b := lipgloss.RoundedBorder()
			b.Right = "├"
			return b
		}()).
		Padding(0, 1).
		Bold(true).
		Render(name)

	line := strings.Repeat("─", max(0, 56-lipgloss.Width(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, line)
}

func renderDeletedAt(f File, datefmt string) string {
	var ts string
	switch datefmt {
	case "absolute":
		ts = f.Timestamp.Format(time.DateTime)
	default:
		ts = humanize.Time(f.Timestamp)
	}
	return stylesSectionStyle.Render(
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			stylesSectionTitleStyle.MarginRight(3).Render("Deleted At"),
			lipgloss.NewStyle().Render(ts)),
	)
}

func renderDeletedWhere(f File) string {
	s := filepath.Dir(f.From)
	w := wordwrap.NewWriter(46)
	w.Breakpoints = []rune{'/', '.'}
	w.KeepNewlines = false
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return stylesSectionStyle.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			stylesSectionTitleStyle.MarginBottom(1).Render("Where it was"),
			lipgloss.NewStyle().Render(w.String())),
	)
}

func renderMetadata(f File) string {
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		stylesSectionStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left, stylesSectionTitleStyle.MarginBottom(1).Render("Size"), renderFileSize(f))),
		stylesSectionStyle.Render(
			lipgloss.JoinVertical(lipgloss.Left, stylesSectionTitleStyle.MarginBottom(1).Render("Type"), renderFileType(f))),
	)
}

func renderFileSize(f File) string {
	var sizeStr string
	size, err := DirSize(f.To)
	if err != nil {
		sizeStr = "(Cannot be calculated)"
	} else {
		sizeStr = humanize.Bytes(uint64(size))
	}
	return sizeStr
}

func renderFileType(f File) string {
	var result string
	fi, err := os.Stat(f.To)
	if err != nil {
		switch {
		case os.IsNotExist(err):
			result = "file has been totally removed"
		default:
			result = err.Error()
		}
	} else {
		if fi.IsDir() {
			result = "(directory)"
		}
	}

	if result == "" {
		mtype, err := mimetype.DetectFile(f.To)
		if err != nil {
			result = err.Error()
		} else {
			result = mtype.String()
		}
	}

	return result
}

func (m model) View() string {
	s := ""

	if m.err != nil {
		s += fmt.Sprintf("error happen %s", m.err)
		return s
	}

	switch m.navState {
	case INVENTORY_LIST:
		s += m.list.View()
	case INVENTORY_DETAILS:
		s += renderInventoryDetails(m)
		s += "\n" + lipgloss.NewStyle().Margin(1, 2).Render(help.New().View(keys))
	case QUITTING:
		return s
	}

	if len(m.choices) > 0 {
		// do not render when selected one or more
		return ""
	}

	return s
}

// TODO: remove?
var (
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	currentItemStyle  = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170")).Width(150)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("#00ff00"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
)

// TODO: remove?
func getAllInventoryItems(files []File) tea.Msg {
	result := []list.Item{}
	for _, file := range files {
		result = append(result, file)
	}
	return GotInventorysMsg{files: result}
}

func getInventoryDetails(file File) tea.Cmd {
	return func() tea.Msg {
		return DetailsMsg{file: file}
	}
}
