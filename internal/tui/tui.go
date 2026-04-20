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
	viewRuns
	viewRunDetail
	viewRunning
	viewRunResult
)

type model struct {
	store     *store.Store
	regs      []store.Registration
	table     table.Model
	mode      viewMode
	status    string
	err       error
	runs      []store.RunRecord // populated in viewRuns
	runsTable table.Model
	detail    *store.RunRecord // active record for viewRunDetail / viewRunResult
	selReg    *store.Registration
	lastPip   string // pipeline name for result view banner
	width     int
	height    int
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
	t.SetStyles(tableStyles())
	m.table = t
	return nil
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).BorderBottom(true).Bold(true)
	s.Selected = s.Selected.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63")).Bold(false)
	return s
}

// loadRuns fills runs/runsTable for the selected registration.
func (m *model) loadRuns(reg store.Registration) error {
	runs, err := m.store.Runs(reg.Slug, 50)
	if err != nil {
		return err
	}
	m.runs = runs
	cols := []table.Column{
		{Title: "WHEN", Width: 19},
		{Title: "RUN ID", Width: 20},
		{Title: "STATUS", Width: 8},
		{Title: "DURATION", Width: 12},
		{Title: "STEPS", Width: 8},
	}
	rows := make([]table.Row, 0, len(runs))
	for _, r := range runs {
		okCount := 0
		for _, s := range r.Steps {
			if s.OK {
				okCount++
			}
		}
		rows = append(rows, table.Row{
			r.StartedAt.Local().Format("2006-01-02 15:04:05"),
			r.RunID,
			r.Status,
			r.Duration.String(),
			fmt.Sprintf("%d/%d", okCount, len(r.Steps)),
		})
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(14),
	)
	t.SetStyles(tableStyles())
	m.runsTable = t
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
		m.detail = msg.rec
		m.lastPip = msg.name
		m.mode = viewRunResult
		_ = m.reload()
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case viewList:
			return m.updateList(msg)
		case viewRuns:
			return m.updateRuns(msg)
		case viewRunDetail, viewRunResult:
			switch msg.String() {
			case "esc", "b", "backspace":
				if m.mode == viewRunDetail {
					m.mode = viewRuns
				} else {
					m.mode = viewList
				}
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		case viewRunning:
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		}
	}
	switch m.mode {
	case viewList:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	case viewRuns:
		var cmd tea.Cmd
		m.runsTable, cmd = m.runsTable.Update(msg)
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
		m.selReg = &reg
		m.lastPip = reg.Name
		if err := m.loadRuns(reg); err != nil {
			m.status = failStyle.Render(fmt.Sprintf("load runs: %v", err))
			return m, nil
		}
		m.mode = viewRuns
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

func (m model) updateRuns(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "b", "backspace":
		m.mode = viewList
		return m, nil
	case "R":
		if m.selReg != nil {
			_ = m.loadRuns(*m.selReg)
			m.status = dimStyle.Render("reloaded runs")
		}
		return m, nil
	case "enter":
		if len(m.runs) == 0 {
			return m, nil
		}
		idx := m.runsTable.Cursor()
		if idx < 0 || idx >= len(m.runs) {
			return m, nil
		}
		rec := m.runs[idx]
		m.detail = &rec
		m.mode = viewRunDetail
		return m, nil
	}
	var cmd tea.Cmd
	m.runsTable, cmd = m.runsTable.Update(msg)
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
		help := helpStyle.Render("↑/↓ move • enter runs • r run • R reload • q quit")
		return strings.Join([]string{header, "", body, "", m.status, help}, "\n")

	case viewRuns:
		title := headerStyle.Render("runs") + "  " + m.lastPip
		body := m.runsTable.View()
		if len(m.runs) == 0 {
			body = dimStyle.Render("no runs yet for this pipeline.")
		}
		help := helpStyle.Render("↑/↓ move • enter detail • R reload • esc back • q quit")
		return strings.Join([]string{header, "", title, "", body, "", m.status, help}, "\n")

	case viewRunning:
		return strings.Join([]string{header, "", m.status, "", helpStyle.Render("(ctrl+c to abort)")}, "\n")

	case viewRunResult:
		return renderRun(header, "run result", m.lastPip, m.detail, m.status) + "\n" + helpStyle.Render("esc back • q quit")

	case viewRunDetail:
		if m.detail == nil {
			return strings.Join([]string{header, "", dimStyle.Render("no run selected."), helpStyle.Render("esc back • q quit")}, "\n")
		}
		return renderRun(header, "run", m.lastPip, m.detail, "") + "\n" + helpStyle.Render("esc back • q quit")
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
