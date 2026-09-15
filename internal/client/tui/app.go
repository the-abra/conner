package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"conner/internal/appdir"
	"conner/internal/client"
	"conner/internal/config"
	"conner/internal/protocol"
	"conner/internal/tor"
	"conner/internal/vaultui"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
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
type downloadDoneMsg struct {
	err      error
	destPath string
}

// ─── Model ────────────────────────────────────────────────────────────────────

type chatRow struct {
	Rendered  string
	MsgId     string
	Acked     bool
	IsOwn     bool
	Reactions map[string][]string // Emoji -> Usernames
}

type reconnectMsg struct {
	cli *client.Client
	err error
}
type model struct {
	cli            *client.Client
	nickname       string
	width          int
	height         int
	input          textarea.Model
	viewport       viewport.Model
	rows           []chatRow // rendered chat rows
	showHelp       bool
	state          string // PENDING, WHITELISTED
	onlineUsers    []string
	isAdmin        bool
	typingUsers    map[string]time.Time
	lastTypingSent time.Time
	autoDownload   bool
	reconnectTick  int
	currentRoom    string
	bootstrapPct   int
	highContrast   bool
	filePane       bool
	showVault      bool
	vaultTab       int // 0 inbox, 1 outbox
	vaultInbox     []vaultui.Entry
	vaultOutbox    []vaultui.Entry
	fileList       []string
	fileCursor     int
	unread         map[string]int
	knownRooms     []string
	statusHint     string
	userCursor     int
	helpVP         viewport.Model
	followTail     bool
	selectHold     bool
	frozenView     string

	// Connection params for retry
	addr   string
	useTor bool
	et     *tor.EmbeddedTor
}

func InitialModel(c *client.Client, nick string, addr string, useTor bool, et *tor.EmbeddedTor) tea.Model {
	m := &model{
		cli:          c,
		nickname:     nick,
		addr:         addr,
		useTor:       useTor,
		et:           et,
		viewport:     viewport.New(0, 0),
		state:        "PENDING",
		typingUsers:  make(map[string]time.Time),
		currentRoom:  config.DefaultRoom,
		unread:       make(map[string]int),
		knownRooms:   []string{config.DefaultRoom},
		highContrast: os.Getenv("CONNER_HIGH_CONTRAST") == "1",
		followTail:   true,
	}
	if c == nil {
		m.state = "BANNED"
	}

	ta := textarea.New()
	ta.Placeholder = "message  ·  Shift+drag copy  ·  Ctrl+Y last line  ·  F1 help"
	ta.Focus()
	ta.CharLimit = 4096
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+j", "alt+enter")
	m.input = ta
	m.applyContrast()
	return m
}

func (m model) Init() tea.Cmd {
	if m.cli == nil {
		return nil
	}
	return tea.Batch(textarea.Blink, m.waitForMsg(), tickVault())
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

	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+s" {
		m.selectHold = !m.selectHold
		if m.selectHold {
			m.statusHint = "SELECT MODE — Shift+drag copy, then Ctrl+S"
			m.frozenView = m.renderLive()
		} else {
			m.frozenView = ""
			m.statusHint = "live view"
		}
		return m, nil
	}

	switch msg := msg.(type) {

	case downloadDoneMsg:
		if msg.err != nil {
			m.appendSystem(fmt.Sprintf("❌ Download failed: %v", msg.err))
		} else {
			m.appendSystem(fmt.Sprintf("✅ Download complete: %s", msg.destPath))
		}
		m.refreshViewport()

	case incomingMsg:
		if msg.Type == config.MsgTypeSystem {
			if strings.Contains(msg.Content, "Waiting for admin approval") {
				m.state = "PENDING"
			} else if strings.Contains(msg.Content, "You have been approved") {
				m.state = ""
				m.statusHint = "approved — you are in"
			} else if strings.Contains(msg.Content, "Approved by shadow bot") {
				m.state = ""
				m.statusHint = "approved"
			} else if strings.Contains(msg.Content, "You have been kicked") {
				m.state = "KICKED"
			} else if strings.Contains(msg.Content, "admin privileges") {
				m.isAdmin = true
			} else if strings.Contains(msg.Content, "Connection closed") {
				if m.state != "KICKED" {
					m.state = "DISCONNECTED"
					m.bootstrapPct = 5
					cmds = append(cmds, m.attemptReconnect(), tickBootstrap())
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
		} else if msg.Type == config.MsgTypeReaction {
			parts := strings.SplitN(msg.Content, "|", 2)
			if len(parts) == 2 {
				targetID := parts[0]
				emoji := parts[1]
				for i := len(m.rows) - 1; i >= 0; i-- {
					if m.rows[i].MsgId == targetID {
						if m.rows[i].Reactions == nil {
							m.rows[i].Reactions = make(map[string][]string)
						}
						// Prevent duplicate reactions from the same user for the same emoji
						exists := false
						for _, u := range m.rows[i].Reactions[emoji] {
							if u == msg.Sender {
								exists = true
								break
							}
						}
						if !exists {
							m.rows[i].Reactions[emoji] = append(m.rows[i].Reactions[emoji], msg.Sender)
							m.refreshViewport()
						}
						break
					}
				}
			}
		} else if msg.Type == config.MsgTypeAck {
			for i := len(m.rows) - 1; i >= 0; i-- {
				if m.rows[i].MsgId == msg.Content {
					m.rows[i].Acked = true
					m.refreshViewport()
					break
				}
			}
		} else if msg.Type == config.MsgTypeTyping {
			if msg.Sender != m.nickname {
				m.typingUsers[msg.Sender] = time.Now()
			}
		} else {
			if msg.Type == config.MsgTypeChat {
				room := msg.OnionAddr
				if room == "" {
					room = config.DefaultRoom
				}
				m.rememberRoom(room)
				if room != m.currentRoom {
					m.unread[room]++
				}
			}
			rendered := m.renderMessage(msg)
			if rendered != "" {
				m.appendLine(rendered)
				m.refreshViewport()
			}
		}
		cmds = append(cmds, m.waitForMsg())
		return m, tea.Batch(cmds...)

	case vaultTickMsg:
		if m.showVault && !m.selectHold {
			m.refreshFiles()
		}
		cmds = append(cmds, tickVault())

	case bootstrapTickMsg:
		if m.state == "DISCONNECTED" && m.bootstrapPct < 95 {
			m.bootstrapPct += 7
			if m.bootstrapPct > 95 {
				m.bootstrapPct = 95
			}
			cmds = append(cmds, tickBootstrap())
		}

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
			} else if m.showVault {
				m.showVault = false
				m.filePane = false
			} else {
				return m, tea.Quit
			}

		case "f1", "ctrl+h":
			m.showHelp = !m.showHelp

		case "ctrl+y", "ctrl+shift+c":
			m.copyLastMessage()

		case "ctrl+v":
			m.pasteClipboard()

		case "ctrl+u":
			m.input.Reset()

		case "ctrl+k":
			m.viewport.LineUp(3)

		case "ctrl+l":
			m.viewport.LineDown(3)

		case "ctrl+g":
			m.followTail = true
			m.viewport.GotoBottom()

		case "ctrl+p":
			m.prefillPrivate()

		case "ctrl+f":
			m.showVault = !m.showVault
			m.filePane = m.showVault
			m.fileCursor = 0
			m.refreshFiles()

		case "ctrl+o":
			if m.showVault {
				m.vaultTab = 1 - m.vaultTab
				m.fileCursor = 0
			}

		case "ctrl+n":
			m.cycleUser(1)

		case "alt+right", "ctrl+right":
			m.switchRoom(nextRoom(m.knownRooms, m.currentRoom))

		case "alt+left", "ctrl+left":
			m.switchRoom(prevRoom(m.knownRooms, m.currentRoom))

		case "tab":
			val := m.input.Value()
			if strings.HasPrefix(val, "/") {
				cmds := []string{"/list", "/private ", "/room ", "/rooms", "/files", "/vault", "/fp", "/trust ", "/approve ", "/kick ", "/block ", "/op ", "/burn", "/contrast", "/copy", "/paste", "/help", "/quit"}
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
			if m.showHelp {
				m.showHelp = false
				break
			}
			if m.showVault {
				m.vaultActivate()
				break
			}
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				break
			}
			m.input.Reset()
			cmds = append(cmds, m.handleInput(val))

		case "up":
			if m.showHelp {
				m.helpVP.LineUp(1)
			} else if m.showVault || m.filePane {
				if m.fileCursor > 0 {
					m.fileCursor--
				}
			} else {
				m.followTail = false
				m.viewport.LineUp(1)
			}
		case "down":
			if m.showHelp {
				m.helpVP.LineDown(1)
			} else if m.showVault || m.filePane {
				n := len(m.vaultActive())
				if n == 0 {
					n = len(m.fileList)
				}
				if m.fileCursor < n-1 {
					m.fileCursor++
				}
			} else {
				m.viewport.LineDown(1)
				if m.viewport.AtBottom() {
					m.followTail = true
				}
			}
		case "pgup":
			m.followTail = false
			m.viewport.HalfViewUp()
		case "pgdown":
			m.viewport.HalfViewDown()
			if m.viewport.AtBottom() {
				m.followTail = true
			}
		}

	case reconnectMsg:
		if msg.err == nil {
			m.cli = msg.cli
			m.state = "" // Connected
			m.reconnectTick = 0
			m.bootstrapPct = 100
			m.appendSystem("reconnected")
			cmds = append(cmds, m.waitForMsg())
		} else {
			m.reconnectTick++
			cmds = append(cmds, m.attemptReconnect())
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 6)
		m.layoutChat()
		if m.showVault {
			m.refreshFiles()
		}
	}

	if !m.showHelp && !shortcutConsumed(msg) {
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)

		if _, ok := msg.(tea.KeyMsg); ok && m.state == "" {
			val := m.input.Value()
			if len(val) > 0 && !strings.HasPrefix(val, "/") {
				if time.Since(m.lastTypingSent) > 2*time.Second {
					typingMsg := protocol.CreateMessage(config.MsgTypeTyping, "", m.nickname)
					select {
					case m.cli.SendChan <- typingMsg:
						m.lastTypingSent = time.Now()
					default:
					}
				}
			}
		}
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

	case val == "/copy":
		m.copyLastMessage()
		return nil

	case val == "/paste":
		m.pasteClipboard()
		return nil

	case val == "/burn":
		m.appendSystem("wipe ~/.conner + cwd leftovers")
		m.refreshViewport()
		go func() {
			_ = appdir.Burn()
			os.Exit(0)
		}()
		return nil

	case val == "/contrast":
		m.highContrast = !m.highContrast
		m.applyContrast()
		m.appendSystem(fmt.Sprintf("high-contrast=%v (also CONNER_HIGH_CONTRAST=1)", m.highContrast))
		m.refreshViewport()
		return nil

	case val == "/files", val == "/vault":
		m.showVault = !m.showVault
		m.filePane = m.showVault
		m.fileCursor = 0
		m.refreshFiles()
		return nil

	case val == "/fp":
		fp := "(none)"
		if m.cli != nil && len(m.cli.SigningPub) > 0 {
			n := min(8, len(m.cli.SigningPub))
			fp = fmt.Sprintf("%x", m.cli.SigningPub[:n])
		}
		m.appendSystem("identity fp prefix: " + fp)
		m.refreshViewport()
		return nil

	case strings.HasPrefix(val, "/approve "), strings.HasPrefix(val, "/kick "), strings.HasPrefix(val, "/block "), strings.HasPrefix(val, "/op "), strings.HasPrefix(val, "/ann "):
		cmd := protocol.CreateMessage(config.MsgTypeCmd, val, m.nickname)
		m.cli.SendChan <- cmd
		m.appendSystem("cmd: " + val)
		m.refreshViewport()
		return nil

	case strings.HasPrefix(val, "/trust "):
		nick := strings.TrimSpace(strings.TrimPrefix(val, "/trust "))
		if nick != "" && m.cli != nil && m.cli.IdentityStore != nil {
			m.cli.IdentityStore.Trust(nick, "")
			m.appendSystem("trusted new identity for " + nick)
			m.refreshViewport()
		}
		return nil

	case strings.HasPrefix(val, "/room "):
		name := strings.TrimSpace(strings.TrimPrefix(val, "/room "))
		if name == "" {
			return nil
		}
		m.switchRoom(name)
		return nil

	case strings.HasPrefix(val, "/react "):
		parts := strings.SplitN(val, " ", 2)
		if len(parts) == 2 {
			emoji := parts[1]
			// Find the last message that is not ours
			var targetID string
			for i := len(m.rows) - 1; i >= 0; i-- {
				if m.rows[i].MsgId != "" && !m.rows[i].IsOwn {
					targetID = m.rows[i].MsgId
					break
				}
			}
			if targetID != "" {
				reactionMsg := protocol.CreateMessage(config.MsgTypeReaction, targetID+"|"+emoji, m.nickname)
				m.cli.SendChan <- reactionMsg

				// Echo locally
				for i := len(m.rows) - 1; i >= 0; i-- {
					if m.rows[i].MsgId == targetID {
						if m.rows[i].Reactions == nil {
							m.rows[i].Reactions = make(map[string][]string)
						}
						m.rows[i].Reactions[emoji] = append(m.rows[i].Reactions[emoji], m.nickname)
						m.refreshViewport()
						break
					}
				}
			} else {
				m.appendSystem("❌ No recent message found to react to.")
				m.refreshViewport()
			}
		}
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
			if m.width > 40 {
				cw -= 20
			}
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
		m.appendRow(chatRow{
			Rendered: m.renderSelf(val),
			MsgId:    chatMsg.MessageId,
			IsOwn:    true,
			Acked:    false,
		})
		m.refreshViewport()
	}
	return nil
}

func (m *model) appendRow(row chatRow) {
	m.rows = append(m.rows, row)
	// Limit history to 1000 lines to prevent memory bloat
	if len(m.rows) > 1000 {
		m.rows = m.rows[len(m.rows)-1000:]
	}
}

func (m *model) appendLine(line string) {
	m.appendRow(chatRow{Rendered: line})
}

func (m *model) appendSystem(text string) {
	m.appendRow(chatRow{Rendered: styleSystem.Render("  · " + text)})
}

func (m *model) layoutChat() {
	side := m.sidebarWidth()
	chatW := m.width - side - 2
	if chatW < 16 {
		chatW = m.width - 2
	}
	m.viewport.Width = chatW
	// title + room tabs + typing + input(3) + hint + padding
	h := m.height - 10
	if h < 4 {
		h = 4
	}
	m.viewport.Height = h
	m.helpVP.Width = min(m.width-4, 72)
	m.helpVP.Height = min(m.height-4, 22)
	m.refreshViewport()
}

func (m model) sidebarWidth() int {
	if m.width < 56 {
		return 0
	}
	if m.filePane {
		return 24
	}
	return 18
}

func (m *model) refreshViewport() {
	var lines []string
	for _, row := range m.rows {
		text := row.Rendered
		if row.IsOwn {
			if row.Acked {
				text += " " + lipgloss.NewStyle().Foreground(clrDim).Render("✓✓")
			} else {
				text += " " + lipgloss.NewStyle().Foreground(clrDim).Render("✓")
			}
		}

		if len(row.Reactions) > 0 {
			var reactionStrs []string
			for emoji, users := range row.Reactions {
				reactionStrs = append(reactionStrs, fmt.Sprintf("%s %d", emoji, len(users)))
			}
			text += "\n    " + lipgloss.NewStyle().Foreground(clrDim).Render("[ "+strings.Join(reactionStrs, "  ")+" ]")
		}

		lines = append(lines, text)
	}
	rawContent := strings.Join(lines, "\n")
	w := m.viewport.Width - 1
	if w < 10 {
		w = 10
	}
	m.viewport.SetContent(rawContent)
	if m.followTail {
		m.viewport.GotoBottom()
	}
}

// ─── Render helpers ───────────────────────────────────────────────────────────

func (m model) renderMarkdown(text string) string {
	w := m.viewport.Width - 6
	if w < 16 {
		w = 16
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(w),
	)
	if err == nil {
		out, err := r.Render(text)
		if err == nil {
			return strings.TrimSpace(out)
		}
	}
	return text
}

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
		if m.width > 40 {
			cw -= 20
		}
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

		sb.WriteString(styleDim.Render(":\n"))
		sb.WriteString(m.renderMarkdown(msg.Content))
		return "  " + sb.String()
	}
}

// renderSelf is used for the local echo of the user's own typed message.
func (m model) renderSelf(content string) string {
	return "  " + styleSelf.Render(m.nickname) + styleDim.Render(":\n") + m.renderMarkdown(content)
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.selectHold && m.frozenView != "" {
		return m.frozenView
	}
	return m.renderLive()
}

func (m model) renderLive() string {
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

	if m.showVault {
		return m.renderVaultPage()
	}

	// ── Overlay: DISCONNECTED ─────────────────────────────────────────────
	if m.state == "DISCONNECTED" {
		disc := fmt.Sprintf(`
		  CONNECTION LOST
		  ─────────────────────────────────────────────
		  Nickname: %s
		  Retry %d · circuit rebuild every 5s
		  Bootstrap ~%d%%
		  [ESC] exit
		`, m.nickname, m.reconnectTick, m.bootstrapPct)
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			styleKickedBox.Render(disc))
	}

	// ── Overlay: HELP ─────────────────────────────────────────────────────
	if m.showHelp {
		adminCmds := ""
		if m.isAdmin {
			adminCmds = "  /approve <nick>    Approve pending user\n  /block <nick>      Ban and disconnect\n  /kick <nick>       Disconnect\n  /op <nick>         Grant admin\n  /ann <msg>         Announcement\n"
		}

		help := fmt.Sprintf(`CONNER — Commands
─────────────────────────────────────
  /list              List online users
  /private <u; msg>   Pairwise DM
  /room <name>       Switch/create room
  /vault             Full-screen files page
  /fp                Show identity prefix
  /trust <nick>      Accept changed key
  /contrast          High-contrast layout
  /burn              Wipe profile & exit
  /quit              Disconnect client

%s  Ctrl+S             Freeze screen to select & copy
  Ctrl+Y             Copy last visible chat line
  Ctrl+F             Vault page
  Ctrl+O             Toggle inbox/outbox tabs
  Ctrl+U             Clear message input
  Ctrl+P             DM selected user
  Ctrl+N             Cycle online users
  Alt+← / Alt+→      Prev / next room
  Ctrl+K / Ctrl+L    Scroll viewport
  Ctrl+G             Jump to latest

  ESC                Close help menu`, adminCmds)
		m.helpVP.SetContent(help)
		box := styleHelp.Width(m.helpVP.Width).Render(m.helpVP.View())
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	// ── Main Chat View (WHITELISTED) ──────────────────────────────────────
	var sb strings.Builder

	mode := "LAN"
	if m.useTor {
		mode = "Tor"
	}
	role := "member"
	if m.isAdmin {
		role = "admin"
	}
	title := fmt.Sprintf(" CONNER %s  %s@%s  #%s  [%s]  %s",
		config.Version, m.nickname, shortHost(m.addr), m.currentRoom, mode, role)
	sb.WriteString(styleTitleBar.Width(m.width - 2).Render(title))
	sb.WriteString("\n")
	sb.WriteString(m.renderRoomTabs())
	sb.WriteString("\n")

	side := m.sidebarWidth()
	chatW := m.width - side - 2
	if chatW < 16 {
		chatW = m.width - 2
		side = 0
	}
	m.viewport.Width = chatW
	chatView := m.viewport.View()

	if side > 0 {
		var userListSB strings.Builder
		userListSB.WriteString(lipgloss.NewStyle().Bold(true).Render("people") + "\n")
		if m.filePane {
			userListSB.WriteString(lipgloss.NewStyle().Bold(true).Render("INBOX") + "\n")
			for i, f := range m.fileList {
				mark := "  "
				if i == m.fileCursor {
					mark = "> "
				}
				userListSB.WriteString(mark + f + "\n")
			}
		}
		for i, u := range m.onlineUsers {
			nick := strings.Split(u, " (")[0]
			mark := "• "
			if i == m.userCursor {
				mark = "> "
			}
			userListSB.WriteString(mark + nick + "\n")
		}
		userListStr := styleUserList.Width(side).Height(m.viewport.Height).Render(userListSB.String())

		mainView := lipgloss.JoinHorizontal(lipgloss.Top, chatView, userListStr)
		sb.WriteString(mainView)
	} else {
		sb.WriteString(chatView)
	}

	sb.WriteString("\n")

	var typing []string
	now := time.Now()
	for user, t := range m.typingUsers {
		if now.Sub(t) < 3*time.Second {
			typing = append(typing, user)
		} else {
			delete(m.typingUsers, user)
		}
	}

	if len(typing) > 0 {
		typingText := strings.Join(typing, ", ")
		if len(typing) == 1 {
			typingText += " is typing..."
		} else {
			typingText += " are typing..."
		}
		sb.WriteString(styleDim.Render("  "+typingText) + "\n")
	}

	// Input box
	sb.WriteString(styleInputBox.Width(m.width - 4).Render(m.input.View()))
	hint := "Ctrl+S freeze to copy · Ctrl+Y last line · Ctrl+F vault · F1"
	if !m.followTail {
		hint = "scrolled up · Ctrl+G latest  ·  " + hint
	}
	if m.statusHint != "" {
		hint = m.statusHint
	}
	sb.WriteString("\n" + styleDim.Render("  "+hint))

	return sb.String()
}
func (m *model) refreshFiles() {
	in, out := "downloads", "uploads"
	if m.cli != nil {
		in = m.cli.InboxDir()
		out = m.cli.OutboxDir()
	}
	m.vaultInbox = vaultui.List(in)
	m.vaultOutbox = vaultui.List(out)
	m.fileList = nil
	for _, e := range m.vaultInbox {
		m.fileList = append(m.fileList, e.Name)
	}
}

func (m model) vaultActive() []vaultui.Entry {
	if m.vaultTab == 1 {
		return m.vaultOutbox
	}
	return m.vaultInbox
}

func (m *model) vaultActivate() {
	ents := m.vaultActive()
	if len(ents) == 0 {
		m.statusHint = "vault empty — drop files in outbox"
		return
	}
	i := m.fileCursor
	if i < 0 || i >= len(ents) {
		i = 0
	}
	dir := "downloads"
	if m.cli != nil {
		if m.vaultTab == 1 {
			dir = m.cli.OutboxDir()
		} else {
			dir = m.cli.InboxDir()
		}
	}
	path := vaultui.SafeJoin(dir, ents[i].Name)
	_ = writeClipboard(path)
	m.statusHint = "copied path: " + path
}

func (m model) renderVaultPage() string {
	var sb strings.Builder
	inDir, outDir := "downloads", "uploads"
	if m.cli != nil {
		inDir, outDir = m.cli.InboxDir(), m.cli.OutboxDir()
	}
	title := fmt.Sprintf(" VAULT  room #%s  ·  ciphertext on hub, plaintext in inbox ", m.currentRoom)
	sb.WriteString(styleTitleBar.Width(m.width - 2).Render(title))
	sb.WriteString("\n")
	inMark, outMark := "inbox", "outbox"
	if m.vaultTab == 0 {
		inMark = "[inbox]"
	} else {
		outMark = "[outbox]"
	}
	sb.WriteString(styleDim.Render(fmt.Sprintf("  %s (%d)    %s (%d)    Ctrl+O switch · Enter copy path · ESC back",
		inMark, len(m.vaultInbox), outMark, len(m.vaultOutbox))))
	sb.WriteString("\n\n")
	sb.WriteString(styleDim.Render("  drop files in: " + outDir + "\n"))
	sb.WriteString(styleDim.Render("  received in:  " + inDir + "\n\n"))
	ents := m.vaultActive()
	if len(ents) == 0 {
		sb.WriteString(styleSystem.Render("  (empty)\n"))
	} else {
		for i, e := range ents {
			line := vaultui.Line(e, i == m.fileCursor, m.width-4)
			if i == m.fileCursor {
				sb.WriteString(styleSelf.Render(line) + "\n")
			} else {
				sb.WriteString(line + "\n")
			}
		}
		sb.WriteString("\n" + styleDim.Render(fmt.Sprintf("  %d files · %s",
			len(ents), vaultui.FormatSize(vaultui.TotalBytes(ents)))))
	}
	sb.WriteString("\n\n" + styleDim.Render("  Hub stores AEAD chunks only. Token is ACL, not a chat key."))
	return sb.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (m *model) attemptReconnect() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		newCli, err := client.Connect(m.nickname, m.addr, m.useTor, m.et)
		return reconnectMsg{cli: newCli, err: err}
	})
}

type vaultTickMsg struct{}

func tickVault() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return vaultTickMsg{} })
}

type bootstrapTickMsg struct{}

func tickBootstrap() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return bootstrapTickMsg{} })
}

func (m *model) rememberRoom(name string) {
	for _, r := range m.knownRooms {
		if r == name {
			return
		}
	}
	m.knownRooms = append(m.knownRooms, name)
}

func (m model) renderRoomTabs() string {
	var parts []string
	for _, r := range m.knownRooms {
		parts = append(parts, formatRoomTab(r, r == m.currentRoom, m.unread[r]))
	}
	line := "  " + strings.Join(parts, "  ")
	return styleDim.Render(line)
}

func (m model) roomBadge() string {
	var parts []string
	for _, r := range m.knownRooms {
		n := m.unread[r]
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s(%d unread)", r, n))
		} else if r == m.currentRoom {
			parts = append(parts, r+"*")
		} else {
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return m.currentRoom
	}
	return strings.Join(parts, ",")
}

func (m *model) switchRoom(name string) {
	if name == "" || m.cli == nil {
		return
	}
	m.currentRoom = name
	m.unread[name] = 0
	m.rememberRoom(name)
	m.cli.SetRoomDirs(name)
	join := protocol.CreateMessage(config.MsgTypeRoomJoin, name, m.nickname)
	select {
	case m.cli.SendChan <- join:
	default:
	}
	m.statusHint = "room #" + name
	m.appendSystem("switched to room #" + name)
	m.refreshViewport()
}

func (m *model) copyLastMessage() {
	text := ""
	for i := len(m.rows) - 1; i >= 0; i-- {
		text = lastVisibleLine(m.rows[i].Rendered)
		if text != "" {
			break
		}
	}
	if text == "" {
		m.statusHint = "nothing to copy"
		return
	}
	if err := writeClipboard(text); err != nil {
		m.statusHint = "clipboard unavailable (install wl-clipboard or xclip)"
		m.appendSystem(m.statusHint)
		return
	}
	m.statusHint = "copied last line"
	m.appendSystem("copied to clipboard")
	m.refreshViewport()
}

func (m *model) pasteClipboard() {
	s, err := readClipboard()
	if err != nil || s == "" {
		m.statusHint = "clipboard empty or unavailable"
		return
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	cur := m.input.Value()
	m.input.SetValue(cur + s)
	m.input.SetCursor(len(cur) + len(s))
	m.statusHint = "pasted"
}

func (m *model) prefillPrivate() {
	nick := m.selectedUser()
	if nick == "" {
		m.statusHint = "no user selected (Ctrl+N)"
		return
	}
	val := "/private " + nick + " "
	m.input.SetValue(val)
	m.input.SetCursor(len(val))
	m.statusHint = "dm " + nick
}

func (m *model) cycleUser(delta int) {
	if len(m.onlineUsers) == 0 {
		return
	}
	m.userCursor = (m.userCursor + delta) % len(m.onlineUsers)
	if m.userCursor < 0 {
		m.userCursor = len(m.onlineUsers) - 1
	}
	m.statusHint = "user " + m.selectedUser()
}

func (m model) selectedUser() string {
	if len(m.onlineUsers) == 0 {
		return ""
	}
	i := m.userCursor
	if i < 0 || i >= len(m.onlineUsers) {
		i = 0
	}
	return strings.Split(m.onlineUsers[i], " (")[0]
}

func (m *model) applyContrast() {
	if !m.highContrast {
		return
	}
	styleSelf = lipgloss.NewStyle().Bold(true).Underline(true)
	styleOther = lipgloss.NewStyle().Bold(true)
	styleSystem = lipgloss.NewStyle().Italic(true).Underline(true)
}
