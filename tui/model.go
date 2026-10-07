package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/snowarch/mak1zu/transport/local"
)

// The look comes from the brand: sumi ink, washi paper, one shu red. Her name
// and the lamp are the only loud things on screen.
var (
	shu   = lipgloss.Color("#E4412B")
	washi = lipgloss.Color("#F3F0EA")
	mute  = lipgloss.Color("#8a857c")

	herName  = lipgloss.NewStyle().Foreground(shu).Bold(true)
	youName  = lipgloss.NewStyle().Foreground(mute)
	faint    = lipgloss.NewStyle().Foreground(mute)
	brand    = lipgloss.NewStyle().Foreground(washi).Background(lipgloss.Color("#121114")).Bold(true).Padding(0, 1)
	errStyle = lipgloss.NewStyle().Foreground(shu)
)

type entry struct {
	who  string // you | her | note
	text string
}

type (
	eventMsg   local.Event
	historyMsg []Line
	streamEnd  struct{}
	cmdDoneMsg struct {
		out string
		ok  bool
		err error
	}
	sayDoneMsg struct{ err error }
	tickMsg    time.Time
)

// Model is the whole screen.
type Model struct {
	conn Conn
	name string // what she is called
	ctx  context.Context

	lines   []entry
	vp      viewport.Model
	in      textarea.Model
	w, h    int
	ready   bool
	typing  time.Time // she is typing until this moment
	dots    int
	stream  <-chan local.Event
	status  string // one dim line above the input: errors, command output hints
	waiting bool   // sent, nothing back yet
	backlog bool   // the "while you were away" divider is already shown
}

// New builds the screen for a connection.
func New(ctx context.Context, c Conn, herName string) Model {
	ta := textarea.New()
	ta.Prompt = "› "
	ta.Placeholder = "say something, or /help"
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.CharLimit = 4000
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ta.Focus()
	return Model{conn: c, name: strings.ToLower(herName), ctx: ctx, in: ta}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.loadHistory(), m.openStream(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(450*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) loadHistory() tea.Cmd {
	return func() tea.Msg {
		l, err := m.conn.History(m.ctx, 30)
		if err != nil {
			return cmdDoneMsg{err: err}
		}
		return historyMsg(l)
	}
}

func (m *Model) openStream() tea.Cmd {
	return func() tea.Msg {
		ch, err := m.conn.Stream(m.ctx)
		if err != nil {
			return cmdDoneMsg{err: err}
		}
		return streamOpened{ch}
	}
}

type streamOpened struct{ ch <-chan local.Event }

func waitEvent(ch <-chan local.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamEnd{}
		}
		return eventMsg(ev)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.refresh()
	case historyMsg:
		for _, l := range msg {
			m.lines = append(m.lines, entry{who: l.Who, text: l.Text})
		}
		m.refresh()
	case streamOpened:
		m.stream = msg.ch
		cmds = append(cmds, waitEvent(m.stream))
	case streamEnd:
		m.status = "the connection to her dropped. quit and reopen to reconnect."
		m.stream = nil
	case eventMsg:
		switch msg.Kind {
		case "typing":
			m.typing = time.Now().Add(6 * time.Second)
		case "backlog", "reply":
			if msg.Kind == "backlog" && !m.backlog {
				m.lines = append(m.lines, entry{who: "note", text: "she wrote while you were away"})
				m.backlog = true
			}
			m.typing, m.waiting = time.Time{}, false
			m.lines = append(m.lines, entry{who: "her", text: msg.Text})
			for _, f := range msg.Files {
				m.lines = append(m.lines, entry{who: "note", text: "she attached " + f + " (open the web chat to download it)"})
			}
			m.refresh()
		}
		if m.stream != nil {
			cmds = append(cmds, waitEvent(m.stream))
		}
	case tickMsg:
		m.dots = (m.dots + 1) % 4
		cmds = append(cmds, tick())
	case sayDoneMsg:
		if msg.err != nil {
			m.waiting = false
			m.status = errStyle.Render("couldn't send: " + msg.err.Error())
		}
	case cmdDoneMsg:
		switch {
		case msg.err != nil:
			m.status = errStyle.Render(msg.err.Error())
		case !msg.ok:
			m.status = "no such command. /help lists them."
		default:
			m.lines = append(m.lines, entry{who: "note", text: strings.TrimSpace(msg.out)})
			m.refresh()
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+d":
			return m, tea.Quit
		case "pgup", "pgdown", "ctrl+u", "ctrl+f":
			var c tea.Cmd
			m.vp, c = m.vp.Update(msg)
			return m, c
		case "enter":
			text := strings.TrimSpace(m.in.Value())
			if text == "" {
				return m, nil
			}
			m.in.Reset()
			m.status = ""
			if strings.HasPrefix(text, "/") {
				return m, m.command(text)
			}
			m.lines = append(m.lines, entry{who: "you", text: text})
			m.waiting = true
			m.refresh()
			c := m.conn
			ctx := m.ctx
			return m, func() tea.Msg { return sayDoneMsg{c.Say(ctx, text)} }
		}
	}
	var c tea.Cmd
	m.in, c = m.in.Update(msg)
	cmds = append(cmds, c)
	// the input grows with what is typed, up to five lines
	if h := min(max(m.in.LineCount(), 1), 5); h != m.in.Height() && m.ready {
		m.in.SetHeight(h)
		m.layout()
		m.refresh()
	}
	return m, tea.Batch(cmds...)
}

const help = `/memories   what she holds about you (memories, open threads, running bits)
/diary      what she wrote about you; "/diary clear" deletes it
/callme X   what she should call you
/link       a code to join your Discord account to this one, or "/link CODE"
/forget N   forget memory N, "thread N", "bit N" or "all"
/remember   tell her something to keep
/mood       how she is
/quit       leave (ctrl+c too). alt+enter writes a new line.`

func (m Model) command(text string) tea.Cmd {
	name, arg, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	name = strings.ToLower(name)
	switch name {
	case "quit", "exit", "q":
		return tea.Quit
	case "help", "?":
		return func() tea.Msg { return cmdDoneMsg{out: help, ok: true} }
	}
	c := m.conn
	ctx := m.ctx
	return func() tea.Msg {
		out, ok, err := c.Command(ctx, name, strings.TrimSpace(arg))
		return cmdDoneMsg{out: out, ok: ok, err: err}
	}
}

func (m *Model) layout() {
	vh := m.h - 3 - m.in.Height() // header, status line, and the input
	if vh < 3 {
		vh = 3
	}
	if !m.ready {
		m.vp = viewport.New(viewport.WithWidth(m.w), viewport.WithHeight(vh))
	} else {
		m.vp.SetWidth(m.w)
		m.vp.SetHeight(vh)
	}
	m.in.SetWidth(m.w - 2)
}

// refresh redraws the conversation and sticks to the bottom.
func (m *Model) refresh() {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.render())
	m.vp.GotoBottom()
}

func (m Model) render() string {
	const col = 6 // label column: "maki" and "you" both fit
	w := m.w - col - 3
	if w < 20 {
		w = 20
	}
	var b strings.Builder
	for i, e := range m.lines {
		if i > 0 {
			b.WriteString("\n")
		}
		switch e.who {
		case "note":
			if strings.Contains(e.text, "\n") { // command output: a quiet block, not a banner
				b.WriteString(faint.Width(m.w - 2).Render(e.text))
			} else {
				b.WriteString(faint.Italic(true).Width(m.w - 2).Render("— " + e.text + " —"))
			}
		case "her":
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, herName.Width(col).Render(shorten(m.name, col-1)), "  ", lipgloss.NewStyle().Width(w).Render(e.text)))
		default:
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, youName.Width(col).Render("you"), "  ", faint.Width(w).Render(e.text)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func (m Model) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	lamp := herName.Render("●")
	head := brand.Render("mak1zu") + "  " + lamp + " " + faint.Render(m.name+" · "+m.conn.Where())

	foot := " "
	switch {
	case m.status != "":
		foot = m.status
	case time.Now().Before(m.typing) || m.waiting:
		foot = faint.Italic(true).Render(m.name + " is typing" + strings.Repeat(".", m.dots))
	}
	clip := lipgloss.NewStyle().MaxWidth(m.w)
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, clip.Render(head), m.vp.View(), clip.Render(foot), m.in.View()))
	v.AltScreen = true
	if cur := m.in.Cursor(); cur != nil {
		cur.Y += lipgloss.Height(head) + m.vp.Height() + 1
		v.Cursor = cur
	}
	v.WindowTitle = fmt.Sprintf("mak1zu · %s", m.name)
	return v
}

// Run starts the screen and returns when the person leaves.
func Run(ctx context.Context, c Conn, herName string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(New(ctx, c, herName), tea.WithContext(ctx)).Run()
	return err
}
