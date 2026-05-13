package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"conner/internal/client"
	"conner/internal/config"
	"conner/internal/protocol"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Colour palette ──────────────────────────────────────────────────────────

var (
	clrSelf    = lipgloss.Color("#A259FF") // purple
	clrOther   = lipgloss.Color("#4B9EFF") // blue
	clrSystem  = lipgloss.Color("#FF8C00") // orange
	clrAdmin   = lipgloss.Color("#00FFCC") // teal
	clrMsgBody = lipgloss.Color("#FFFFFF") // white text
	clrDim     = lipgloss.Color("#555555") // gray
	clrGreen   = lipgloss.Color("#FFFFFF") // CHANGED TO WHITE
	clrDkGreen = lipgloss.Color("#AAAAAA") // CHANGED TO GRAY
	clrYellow  = lipgloss.Color("#FFFF00")
	clrRed     = lipgloss.Color("#FF0000")
	clrBlue    = lipgloss.Color("#4B9EFF")
	clrInput   = lipgloss.Color("#FFFFFF") // White input text

	styleSelf    = lipgloss.NewStyle().Foreground(clrSelf).Bold(true)
	styleOther   = lipgloss.NewStyle().Foreground(clrOther).Bold(true)
	styleSystem  = lipgloss.NewStyle().Foreground(clrSystem).Italic(true)
	styleDim     = lipgloss.NewStyle().Foreground(clrDim)
	styleBody    = lipgloss.NewStyle().Foreground(clrMsgBody)
	styleAdminTS = lipgloss.NewStyle().Foreground(clrAdmin)

	styleTitleBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrGreen).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(clrDkGreen).
			MarginBottom(1)

	styleUserList = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(clrDim).
			Padding(0, 1).
			Foreground(clrMsgBody)

	stylePendingBox = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(clrYellow).
			Padding(1, 2).
			Foreground(clrMsgBody)

	styleApprovedBox = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(clrGreen).
				Padding(1, 2).
				Foreground(clrMsgBody)

	styleKickedBox = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(clrRed).
			Padding(1, 2).
			Foreground(clrMsgBody)

	styleInputBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrDkGreen).
			Padding(0, 1)

	styleHelp = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(clrGreen).
			Padding(1, 2).
			Foreground(lipgloss.Color("#AAAAAA"))
)

// ─── Message types ────────────────────────────────────────────────────────────

type incomingMsg *protocol.ChatMessage
type uploadDoneMsg struct {
	err      error
	filename string
	b64data  string
}

// ─── Model ────────────────────────────────────────────────────────────────────

type model struct {
	cli            *client.Client
	nickname       string
	width          int
	height         int
	input          textinput.Model
	viewport       viewport.Model
	lines          []string // rendered lines in the viewport
	showHelp       bool
	state          string // PENDING, WHITELISTED
	onlineUsers    []string
	isAdmin        bool
}

func InitialModel(c *client.Client, nick string) tea.Model {
	m := &model{
		cli:      c,
		nickname: nick,
		viewport: viewport.New(0, 0),
		state:    "PENDING",
	}
	if c == nil {
		m.state = "BANNED"
	}

	ti := textinput.New()
	ti.Placeholder = "Type a message... (/help for commands)"
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80
	m.input = ti
	m.input.PromptStyle = lipgloss.NewStyle().Foreground(clrGreen)
	m.input.TextStyle = lipgloss.NewStyle().Foreground(clrInput)
	return m
}

func (m model) Init() tea.Cmd {
	if m.cli == nil {
		return nil
	}
	return tea.Batch(textinput.Blink, m.waitForMsg())
}

func (m model) waitForMsg() tea.Cmd {
	if m.cli == nil {
		return nil
	}
	return func() tea.Msg {
		return func() incomingMsg {
			msg := <-m.cli.UpdateChan
			return msg
		}()
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case incomingMsg:
		if msg.Type == config.MsgTypeSystem {
			if strings.Contains(msg.Content, "Waiting for admin approval") {
				m.state = "PENDING"
			} else if strings.Contains(msg.Content, "You have been approved") {
				m.state = "APPROVED"
			} else if strings.Contains(msg.Content, "Approved by shadow bot") {
				m.state = "SHADOW_APPROVED"
			} else if strings.Contains(msg.Content, "You have been kicked") {
				m.state = "KICKED"
			} else if strings.Contains(msg.Content, "admin privileges") {
				m.isAdmin = true
			} else if strings.Contains(msg.Content, "Connection closed") {
				if m.state != "KICKED" {
					m.state = "DISCONNECTED"
				}
			}
		}
		if msg.Type == config.MsgTypeUserList {
			rawUsers := strings.Split(msg.Content, ",")
			m.onlineUsers = nil
			for _, u := range rawUsers {
				if strings.TrimSpace(u) != "" {
					parts := strings.SplitN(u, "|", 2)
					m.onlineUsers = append(m.onlineUsers, parts[0])
				}
			}
		}
		if msg.Type != config.MsgTypeUserList {
			rendered := m.renderMessage(msg)
			if rendered != "" {
				m.appendLine(rendered)
				m.refreshViewport()
			}
		}
		cmds = append(cmds, m.waitForMsg())
		return m, tea.Batch(cmds...)


	case tea.MouseMsg:
		if !m.showHelp {
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.KeyMsg:

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			if m.showHelp {
				m.showHelp = false
			} else {
				return m, tea.Quit
			}

		case "f1":
			m.showHelp = !m.showHelp

		case "tab":
			val := m.input.Value()
			if strings.HasPrefix(val, "/") {
				cmds := []string{"/list", "/private ", "/upload ", "/download ", "/help", "/quit"}
				current := strings.Split(val, " ")[0]

				var matches []string
				for _, c := range cmds {
					if strings.HasPrefix(c, current) {
						matches = append(matches, c)
					}
				}

				if len(matches) > 0 {
					// Simple rotation
					found := -1
					for i, m := range matches {
						if m == current || (strings.HasSuffix(m, " ") && m == current+" ") {
							found = i
							break
						}
					}
					next := matches[(found+1)%len(matches)]
					m.input.SetValue(next)
					m.input.SetCursor(len(next))
				}
			}

		case "enter":
			if m.state == "APPROVED" || m.state == "SHADOW_APPROVED" {
				m.state = ""
				return m, nil
			}
			if m.showHelp {
				m.showHelp = false
				break
			}
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				break
			}
			m.input.SetValue("")
			cmds = append(cmds, m.handleInput(val))

		case "up", "down", "pgup", "pgdown":
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 6
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 6
		m.refreshViewport()
	}

	if !m.showHelp {
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) handleInput(val string) tea.Cmd {
	switch {
	case val == "/quit":
		return tea.Quit

	case val == "/help":
		m.showHelp = true
		return nil

	case val == "/burn":
		m.appendSystem("🔥 Initiating Panic Switch...")
		m.refreshViewport()
		go func() {
			os.Remove("identity.key")
			os.Exit(0)
		}()
		return nil

	case strings.HasPrefix(val, "/private"):
		parts := strings.SplitN(val, " ", 3)
		if len(parts) == 3 {
			target := parts[1]
			content := parts[2]
			msg := protocol.CreateMessage(config.MsgTypePrivate, content, m.nickname)
			msg.ReplyTo = target
			m.cli.SendChan <- msg
			// Local echo for sent PM
			cw := m.width - 2
			if m.width > 40 { cw -= 20 }
			m.appendLine(lipgloss.NewStyle().
				Foreground(clrAdmin).
				Width(cw - 2).
				Align(lipgloss.Right).
				Render("[PM to " + target + "] " + content))
			m.refreshViewport()
		} else {
			m.appendSystem("Usage: /private <nick> <msg>")
			m.refreshViewport()
		}
		return nil

	case val == "/list":
		m.appendSystem("Online Users: " + strings.Join(m.onlineUsers, ", "))
		m.refreshViewport()
		return nil

	default:
		// Regular chat message
		chatMsg := protocol.CreateMessage(config.MsgTypeChat, val, m.nickname)
		m.cli.SendChan <- chatMsg
		// Echo own message locally
		m.appendLine(m.renderSelf(val))
		m.refreshViewport()
	}
	return nil
}

func (m *model) appendLine(line string) {
	m.lines = append(m.lines, line)
	// Limit history to 1000 lines to prevent memory bloat
	if len(m.lines) > 1000 {
		m.lines = m.lines[len(m.lines)-1000:]
	}
}

func (m *model) appendSystem(text string) {
	m.appendLine(styleSystem.Render("  · " + text))
}

func (m *model) refreshViewport() {
	content := lipgloss.NewStyle().Width(m.width - 2).Render(strings.Join(m.lines, "\n"))
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

// ─── Render helpers ───────────────────────────────────────────────────────────

// renderMessage converts a ChatMessage into a styled terminal line.
// Rules:
//   - System / server notices  → orange, minimalist (no sender name)
//   - Own chat message         → purple name, white body
//   - Other chat messages      → blue name, white body
//   - Timestamp                → shown only when msg.IsAdmin == true
func (m model) renderMessage(msg *protocol.ChatMessage) string {
	switch msg.Type {
	case config.MsgTypeSystem, config.MsgTypeJoin:
		// Minimalist system line — just orange text, no heavy formatting
		return styleSystem.Render("  · " + msg.Content)

	case config.MsgTypePrivate:
		// Private messages: teal accent, right aligned
		cw := m.width - 2
		if m.width > 40 { cw -= 20 }
		pm := lipgloss.NewStyle().
			Foreground(clrAdmin).
			Width(cw - 2).
			Align(lipgloss.Right).
			Render("[PM] " + msg.Sender + ": " + msg.Content)
		return pm

	default: // MsgTypeChat and anything else
		var sb strings.Builder

		// Timestamp — only when sender was an admin
		if msg.IsAdmin && msg.Timestamp != "" {
			// Parse down to HH:MM
			ts := msg.Timestamp
			if t, err := time.Parse("2006-01-02 15:04:05", msg.Timestamp); err == nil {
				ts = t.Format("15:04")
			}
			sb.WriteString(styleAdminTS.Render(ts))
			sb.WriteString(styleDim.Render(" "))
		}

		// Name
		if msg.Sender == m.nickname {
			sb.WriteString(styleSelf.Render(msg.Sender))
		} else {
			sb.WriteString(styleOther.Render(msg.Sender))
		}

		sb.WriteString(styleDim.Render(": "))
		sb.WriteString(styleBody.Render(msg.Content))
		return "  " + sb.String()
	}
}

// renderSelf is used for the local echo of the user's own typed message.
func (m model) renderSelf(content string) string {
	return "  " + styleSelf.Render(m.nickname) + styleDim.Render(": ") + styleBody.Render(content)
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return "Connecting…"
	}


	// ── Overlay: PENDING ──────────────────────────────────────────────────
	if m.state == "PENDING" {
		pending := fmt.Sprintf(`
  CONNECTION ESTABLISHED
  ─────────────────────────────────────────────
  Your nickname: %s
  
  Please wait for an administrator to approve
  your connection.
  
  [ESC] Disconnect
`, m.nickname)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			stylePendingBox.Render(pending))
	}

	// ── Overlay: APPROVED ─────────────────────────────────────────────────
	if m.state == "APPROVED" {
		approved := fmt.Sprintf(`
  ACCESS GRANTED
  ─────────────────────────────────────────────
  You have been approved by an admin!
  
  Nickname: %s
  
  Press [ ENTER ] to join the chat.
`, m.nickname)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleApprovedBox.Render(approved))
	}
	// ── Overlay: BANNED ───────────────────────────────────────────────────
	if m.state == "BANNED" {
		banned := fmt.Sprintf(`
  ACCESS DENIED
  ─────────────────────────────────────────────
  You have been permanently banned from
  this server.
  
  Identity: %s
  
  [Ctrl+C] or [ESC] to Exit.
`, m.nickname)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleKickedBox.Render(banned))
	}

	// ── Overlay: KICKED ───────────────────────────────────────────────────
	if m.state == "KICKED" {
		kicked := fmt.Sprintf(`
  CONNECTION TERMINATED
  ─────────────────────────────────────────────
  You have been kicked from the server
  by an administrator.
  
  Nickname: %s
  
  [Ctrl+C] or [ESC] to Exit.
`, m.nickname)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleKickedBox.Render(kicked))
	}

	// ── Overlay: SHADOW APPROVED ──────────────────────────────────────────
	if m.state == "SHADOW_APPROVED" {
		approved := fmt.Sprintf(`
  ACCESS GRANTED (SHADOW)
  ─────────────────────────────────────────────
  You have been approved by an admin!
  
  Nickname: %s
  
  Press [ ENTER ] to join the chat.
`, m.nickname)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleKickedBox.Render(approved)) // Use red box for shadow
	}

	// ── Overlay: DISCONNECTED ─────────────────────────────────────────────
	if m.state == "DISCONNECTED" {
		disc := fmt.Sprintf(`
  CONNECTION LOST
  ─────────────────────────────────────────────
  The connection to the server was lost.
  
  Nickname: %s
  
  [Ctrl+C] or [ESC] to Exit.
`, m.nickname)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleKickedBox.Render(disc))
	}

	// ── Overlay: HELP ─────────────────────────────────────────────────────
	if m.showHelp {
		adminCmds := ""
		if m.isAdmin {
			adminCmds = `  /connect <nick>    Approve a pending user
  /block <nick>      Block and disconnect user
  /kick <nick>       Disconnect a user
  /ann <msg>         Send global announcement
  /op <nick>         Grant admin permissions
`
		}

		help := fmt.Sprintf(`
  CONNER CLIENT — Commands
  ─────────────────────────────────────
  /list              List online users
  /private <u> <msg> Send private message
  /share             Share a file/folder via P2P
  /download <id>     Download a file
  /burn              Panic switch: wipe identity & files
%s  /help              Show this menu
  /quit              Disconnect

  [Tab]              Auto-complete / commands
  [Shift+Mouse]      Select & Copy text
  [ESC / Enter / F1] Close this menu
  [↑ ↓ PgUp PgDn]    Scroll chat
  [Ctrl+C]           Force quit
`, adminCmds)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleHelp.Render(help))
	}

	// ── Main Chat View (WHITELISTED) ──────────────────────────────────────
	var sb strings.Builder

	// Title bar
	sb.WriteString(styleTitleBar.Width(m.width - 2).
		Render(fmt.Sprintf(" CONNER  ·  %s", m.nickname)))
	sb.WriteString("\n")

	// Split View: Chat (left) | Users (right)
	userListWidth := 20
	chatWidth := m.width - userListWidth - 2
	if chatWidth < 20 {
		chatWidth = m.width - 2
		userListWidth = 0
	}

	m.viewport.Width = chatWidth
	chatView := m.viewport.View()

	if userListWidth > 0 {
		var userListSB strings.Builder
		userListSB.WriteString(lipgloss.NewStyle().Bold(true).Underline(true).Render("ONLINE USERS") + "\n")
		for _, u := range m.onlineUsers {
			// Only show nickname (part before first '(' if any, or just clean it)
			nick := strings.Split(u, " (")[0]
			userListSB.WriteString("• " + nick + "\n")
		}
		userListStr := styleUserList.Width(userListWidth).Height(m.viewport.Height).Render(userListSB.String())

		mainView := lipgloss.JoinHorizontal(lipgloss.Top, chatView, userListStr)
		sb.WriteString(mainView)
	} else {
		sb.WriteString(chatView)
	}

	sb.WriteString("\n")

	// Input box
	sb.WriteString(styleInputBox.Width(m.width - 4).Render(m.input.View()))

	return sb.String()
}
