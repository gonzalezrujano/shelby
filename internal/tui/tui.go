package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"shelby/internal/config"
	"shelby/internal/runner"
	"shelby/internal/store"
)

// Run launches the interactive dashboard bound to st.
func Run(st *store.Store) error {
	m, err := initialModel(st)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
	viewRunning
	viewRunResult
)

type model struct {
	store   *store.Store
	regs    []store.Registration
	table   table.Model
	mode    viewMode
	status  string
	err     error
	last    *store.RunRecord // used in detail + run result
	lastPip string           // pipeline name for result view
	width   int
	height  int
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	titleStyle  = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("63")).Foreground(lipgloss.Color("230")).Padding(0, 1)
)

func initialModel(st *store.Store) (model, error) {
	m := model{store: st, mode: viewList}
	if err := m.reload(); err != nil {
		return m, err
	}
	return m, nil
}

func (m *model) reload() error {
	regs, err := m.store.List()
	if err != nil {
		return err
	}
	m.regs = regs

	cols := []table.Column{
		{Title: "SLUG", Width: 24},
		{Title: "NAME", Width: 28},
		{Title: "INTERVAL", Width: 10},
		{Title: "LAST RUN", Width: 19},
		{Title: "STATUS", Width: 8},
	}
	rows := make([]table.Row, 0, len(regs))
	for _, r := range regs {
		interval := "?"
		if p, err := config.Load(r.Path); err == nil {
			interval = p.Interval.String()
		}
		status := "-"
		lastRun := "-"
		if last, _ := m.store.LastRun(r.Slug); last != nil {
			status = last.Status
			lastRun = last.StartedAt.Local().Format("2006-01-02 15:04:05")
		}
		rows = append(rows, table.Row{r.Slug, r.Name, interval, lastRun, status})
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(12),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).BorderBottom(true).Bold(true)
	s.Selected = s.Selected.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63")).Bold(false)
	t.SetStyles(s)
	m.table = t
	return nil
}

func (m model) Init() tea.Cmd { return nil }

// runDoneMsg fires when an async pipeline run finishes.
type runDoneMsg struct {
	slug string
	name string
	rec  *store.RunRecord
	err  error
}

func (m model) runSelectedCmd() tea.Cmd {
	if len(m.regs) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.regs) {
		return nil
	}
	reg := m.regs[idx]
	st := m.store
	return func() tea.Msg {
		p, err := config.Load(reg.Path)
		if err != nil {
			return runDoneMsg{slug: reg.Slug, name: reg.Name, err: err}
		}
		res := runner.Execute(context.Background(), p, st, reg.Slug)
		last, _ := st.LastRun(reg.Slug)
		return runDoneMsg{slug: reg.Slug, name: reg.Name, rec: last, err: res.Err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case runDoneMsg:
		if msg.err != nil {
			m.status = failStyle.Render(fmt.Sprintf("✗ %s: %v", msg.name, msg.err))
		} else {
			m.status = okStyle.Render(fmt.Sprintf("✓ %s ran ok", msg.name))
		}
		m.last = msg.rec
		m.lastPip = msg.name
		m.mode = viewRunResult
		_ = m.reload()
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case viewList:
			return m.updateList(msg)
		case viewDetail, viewRunResult:
			switch msg.String() {
			case "esc", "b", "backspace":
				m.mode = viewList
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		case viewRunning:
			// ignore most keys; allow Ctrl+C bail
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		}
	}
	if m.mode == viewList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "r":
		if len(m.regs) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		m.status = dimStyle.Render(fmt.Sprintf("running %s…", m.regs[idx].Name))
		m.mode = viewRunning
		return m, m.runSelectedCmd()
	case "enter":
		if len(m.regs) == 0 {
			return m, nil
		}
		reg := m.regs[m.table.Cursor()]
		last, _ := m.store.LastRun(reg.Slug)
		m.last = last
		m.lastPip = reg.Name
		m.mode = viewDetail
		return m, nil
	case "R":
		_ = m.reload()
		m.status = dimStyle.Render("reloaded")
		return m, nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	header := titleStyle.Render(" SHELBY ") + "  " + dimStyle.Render(fmt.Sprintf("%d pipelines • %s", len(m.regs), m.store.Root))
	switch m.mode {
	case viewList:
		body := m.table.View()
		if len(m.regs) == 0 {
			body = dimStyle.Render("no pipelines registered.\nuse:  shelby add <file.yaml>")
		}
		help := helpStyle.Render("↑/↓ move • enter detail • r run • R reload • q quit")
		return strings.Join([]string{header, "", body, "", m.status, help}, "\n")

	case viewRunning:
		return strings.Join([]string{header, "", m.status, "", helpStyle.Render("(ctrl+c to abort)")}, "\n")

	case viewRunResult:
		return renderRun(header, "run result", m.lastPip, m.last, m.status) + "\n" + helpStyle.Render("esc back • q quit")

	case viewDetail:
		if m.last == nil {
			return strings.Join([]string{header, "", dimStyle.Render("no runs yet for this pipeline."), helpStyle.Render("esc back • q quit")}, "\n")
		}
		return renderRun(header, "last run", m.lastPip, m.last, "") + "\n" + helpStyle.Render("esc back • q quit")
	}
	return ""
}

func renderRun(header, label, name string, r *store.RunRecord, status string) string {
	if r == nil {
		return strings.Join([]string{header, "", failStyle.Render("no run record")}, "\n")
	}
	var b strings.Builder
	fmt.Fprintln(&b, header)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, headerStyle.Render(label)+"  "+name)
	statusStyled := okStyle.Render(r.Status)
	if r.Status != "ok" {
		statusStyled = failStyle.Render(r.Status)
	}
	fmt.Fprintf(&b, "run: %s   status: %s   duration: %s\n", r.RunID, statusStyled, r.Duration)
	fmt.Fprintf(&b, "started: %s\n\n", r.StartedAt.Local().Format("2006-01-02 15:04:05"))
	if r.Error != "" {
		fmt.Fprintln(&b, failStyle.Render("error: ")+truncate(r.Error, 240))
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, headerStyle.Render("steps"))
	for _, s := range r.Steps {
		glyph := okStyle.Render("✓")
		if !s.OK {
			glyph = failStyle.Render("✗")
		}
		fmt.Fprintf(&b, "  %s %-14s %-12s %s\n", glyph, s.ID, s.Type, s.Duration)
		if s.Error != "" {
			fmt.Fprintf(&b, "      %s\n", dimStyle.Render(truncate(s.Error, 180)))
		}
	}
	if len(r.Output) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, headerStyle.Render("output"))
		bs, _ := json.MarshalIndent(r.Output, "  ", "  ")
		fmt.Fprintln(&b, "  "+string(bs))
	}
	if status != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, status)
	}
	return b.String()
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
