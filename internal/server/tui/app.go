package tui

import (
	"fmt"
	"strings"
	"time"

	"conner/internal/config"
	"conner/internal/protocol"
	"conner/internal/server"
	"conner/internal/server/sysmon"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Theme ───────────────────────────────────────────────────────────────────

var (
	clrGreen    = lipgloss.Color("#00FF41")
	clrDkGreen  = lipgloss.Color("#008F11")
	clrGray     = lipgloss.Color("#888888")
	clrDkGray   = lipgloss.Color("#222222")
	clrWhite    = lipgloss.Color("#DDDDDD")
	clrRed      = lipgloss.Color("#FF3333")
	clrCyan     = lipgloss.Color("#00FFFF")
	clrYellow   = lipgloss.Color("#FFD700")

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrGreen).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(clrDkGreen).
			MarginBottom(1)

	styleActiveTab = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(clrGreen).
			Padding(0, 2).
			MarginRight(1)

	styleInactiveTab = lipgloss.NewStyle().
				Foreground(clrGreen).
				Background(clrDkGray).
				Padding(0, 2).
				MarginRight(1)

	styleInput = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrDkGreen).
			Padding(0, 1)

	styleStatus = lipgloss.NewStyle().
			Foreground(clrGray).
			Italic(true)

	styleHelp = lipgloss.NewStyle().
			Foreground(clrGray)

	styleFileLine = lipgloss.NewStyle().
			Foreground(clrWhite)

	styleFileSelected = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(clrRed).
				Bold(true)

	styleSysMsg = lipgloss.NewStyle().Foreground(clrCyan).Italic(true)
	styleWL     = lipgloss.NewStyle().Foreground(clrGreen)
	styleBL     = lipgloss.NewStyle().Foreground(clrRed)
)

// ─── Tabs ────────────────────────────────────────────────────────────────────

const (
	tabDashboard = iota
	tabWhitelist
	tabBlacklist
	tabClients
	tabFiles
	tabSystem
)

var tabNames = []string{"Dashboard", "Whitelist", "Blacklist", "Clients", "Files", "System"}

// ─── Model ───────────────────────────────────────────────────────────────────

type Model struct {
	srv         *server.Server
	tab         int
	width       int
	height      int
	input       textinput.Model
	viewport    viewport.Model
	filesCursor int            // cursor row in Files tab
	statusMsg   string
	sysSnap     sysmon.Snapshot // cached system metrics
}

func InitialModel(s *server.Server) tea.Model {
	ti := textinput.New()
	ti.Placeholder = "/connect <user>  /block <user>  /kick <user>  /ann <msg>"
	ti.PromptStyle = lipgloss.NewStyle().Foreground(clrGreen)
	ti.TextStyle = lipgloss.NewStyle().Foreground(clrWhite)
	ti.Focus()

	vp := viewport.New(0, 0)

	return Model{
		srv:      s,
		input:    ti,
		viewport: vp,
	}
}

// ─── Init ────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickCmd())
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tickMsg:
		// Refresh system snapshot on every tick (1s)
		m.sysSnap = sysmon.Collect()
		cmds = append(cmds, tickCmd())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		inputH := 3
		tabBarH := 2
		statusH := 1
		helpH := 1
		vH := m.height - inputH - tabBarH - statusH - helpH - 2
		if vH < 1 {
			vH = 1
		}
		m.viewport.Width = m.width - 2
		m.viewport.Height = vH
		m.input.Width = m.width - 6

	case tea.MouseMsg:
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c":
			m.srv.Running = false
			return m, tea.Quit

		case "tab":
			val := m.input.Value()
			if strings.HasPrefix(val, "/") {
				cmds := []string{"/ann ", "/kick ", "/block ", "/unblock ", "/whitelist ", "/connect ", "/help"}
				current := strings.Split(val, " ")[0]
				
				var matches []string
				for _, c := range cmds {
					if strings.HasPrefix(c, current) {
						matches = append(matches, c)
					}
				}

				if len(matches) > 0 {
					found := -1
					for i, match := range matches {
						if match == current || (strings.HasSuffix(match, " ") && match == current+" ") {
							found = i
							break
						}
					}
					next := matches[(found+1)%len(matches)]
					m.input.SetValue(next)
					m.input.SetCursor(len(next))
					break // Don't switch tabs if completing
				}
			}
			// Fallthrough to tab switching
			m.tab = (m.tab + 1) % len(tabNames)
			m.filesCursor = 0
			m.viewport.GotoTop()

		case "shift+tab":
			m.tab = (m.tab - 1 + len(tabNames)) % len(tabNames)
			m.filesCursor = 0
			m.viewport.GotoTop()

		case "up", "k":
			if m.tab == tabFiles {
				files := m.srv.GetMediaList()
				if m.filesCursor > 0 {
					m.filesCursor--
				}
				_ = files
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}

		case "down", "j":
			if m.tab == tabFiles {
				files := m.srv.GetMediaList()
				if m.filesCursor < len(files)-1 {
					m.filesCursor++
				}
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}

		case "pgup", "pgdown":
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)

		case "delete", "d":
			if m.tab == tabFiles {
				files := m.srv.GetMediaList()
				if m.filesCursor < len(files) {
					entry := files[m.filesCursor]
					if m.srv.DeleteMedia(entry.ID) {
						m.statusMsg = fmt.Sprintf("Deleted: %s", entry.Filename)
						// Clamp cursor: if we deleted the last item,
						// move up; if list is now empty, reset to 0.
						remaining := len(files) - 1
						if remaining <= 0 {
							m.filesCursor = 0
						} else if m.filesCursor >= remaining {
							m.filesCursor = remaining - 1
						}
					}
				}
			}

		case "enter":
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				break
			}
			m.input.SetValue("")
			m.statusMsg = ""
			m.executeAdminCommand(val)
		}
	}

	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// executeAdminCommand processes server-side CLI commands from the admin panel.
func (m *Model) executeAdminCommand(val string) {
	parts := strings.SplitN(val, " ", 3)
	if len(parts) == 0 {
		return
	}
	cmd := parts[0]

	switch cmd {
	case "/connect", "/approve":
		if len(parts) >= 2 {
			c := m.srv.ClientManager.GetClientByNickname(parts[1])
			if c != nil {
				c.State = "WHITELISTED"
				m.srv.SendSystemMessage(c, "✅ You have been approved by an admin. Welcome!")
				m.srv.Log("Admin approved: " + parts[1])
				m.srv.AddEvent("✅", "Admin approved: "+parts[1]+" → WHITELIST")
				m.statusMsg = "✓ Approved: " + parts[1]
			} else {
				m.statusMsg = "User not found: " + parts[1]
			}
		}

	case "/block", "/blacklist":
		if len(parts) >= 2 {
			c := m.srv.ClientManager.GetClientByNickname(parts[1])
			if c != nil {
				c.State = "BLACKLISTED"
				m.srv.SendSystemMessage(c, "✅ Approved by bot. Welcome!")
				m.srv.Log("Admin blacklisted (shadow): " + parts[1])
				m.statusMsg = "✗ Blacklisted (Shadow): " + parts[1]
				m.srv.AddEvent("⛔", "Admin blacklisted "+parts[1]+" → SHADOW ROOM")
			} else {
				m.statusMsg = "User not found: " + parts[1]
			}
		}

	case "/kick":
		if len(parts) >= 2 {
			c := m.srv.ClientManager.GetClientByNickname(parts[1])
			if c != nil {
				m.srv.Log("Admin kicked: " + parts[1])
				m.statusMsg = "⚡ Kicked: " + parts[1]
				c.Conn.Close()
			} else {
				m.statusMsg = "User not found: " + parts[1]
			}
		}

	case "/ann":
		if len(parts) >= 2 {
			annText := strings.Join(parts[1:], " ")
			annMsg := protocol.CreateMessage(config.MsgTypeSystem,
				"📢 [SERVER ANNOUNCEMENT]: "+annText, "SERVER")
			// Broadcast to all active rooms
			for _, state := range []string{"WHITELISTED", "BLACKLISTED"} {
				m.srv.BroadcastToState(state, annMsg, "")
			}
			m.srv.Log("[ANN] " + annText)
			m.srv.AddEvent("📢", "Announcement: "+annText)
			m.statusMsg = "📢 Announced to all rooms"
		}

	case "/purge":
		m.srv.DBManager.Clear()
		m.srv.BlacklistDB.Clear()
		m.srv.Log("Admin purged all messages")
		m.statusMsg = "🗑 All messages purged"

	default:
		m.statusMsg = "Unknown command: " + cmd
		m.srv.Log("Admin unknown command: " + val)
	}
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Loading CONNER Admin Panel..."
	}

	var sb strings.Builder

	// ── Tab bar ──────────────────────────────────────────────────────────────
	var tabs []string
	for i, name := range tabNames {
		if i == m.tab {
			tabs = append(tabs, styleActiveTab.Render(name))
		} else {
			tabs = append(tabs, styleInactiveTab.Render(name))
		}
	}
	tabBar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	uptime := time.Since(m.srv.Stats.StartTime).Round(time.Second)
	tabBarLine := lipgloss.NewStyle().Width(m.width).Render(
		tabBar + styleStatus.Render(fmt.Sprintf("  uptime:%s", uptime)))
	sb.WriteString(tabBarLine + "\n\n")

	// ── Content ──────────────────────────────────────────────────────────────
	content := m.renderTab()
	m.viewport.SetContent(content)
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")

	// ── Status bar ───────────────────────────────────────────────────────────
	if m.statusMsg != "" {
		sb.WriteString(styleStatus.Render(" " + m.statusMsg))
	} else {
		sb.WriteString(styleHelp.Render(" [Tab] switch  [↑↓] scroll  [D/Del] delete file  [Ctrl+C] quit"))
	}
	sb.WriteString("\n")

	// ── Input ────────────────────────────────────────────────────────────────
	sb.WriteString(styleInput.Render(m.input.View()) + "\n")

	// ── Help Footer ──────────────────────────────────────────────────────────
	help := styleGray("  [Tab] autocomplete / switch   [S-Tab] prev tab   [Shift+Mouse] select & copy")
	sb.WriteString(help)

	return sb.String()
}

func (m Model) renderTab() string {
	switch m.tab {

	case tabDashboard:
		colW := (m.width - 6) / 2
		if colW < 20 {
			colW = 20
		}

		// ── LEFT COLUMN: Server Stats ─────────────────────────────────────────
		var left strings.Builder
		left.WriteString(styleTitle.Width(colW).Render(" SERVER STATS ") + "\n")

		uptime := time.Since(m.srv.Stats.StartTime).Round(time.Second)
		clients := m.srv.ClientManager.GetAllClients()
		wl, bl, pend := 0, 0, 0
		for _, c := range clients {
			switch c.State {
			case "WHITELISTED":
				wl++
			case "BLACKLISTED":
				bl++
			default:
				pend++
			}
		}

		stat := func(label, val string) string {
			return fmt.Sprintf("  %-18s %s\n", label, lipgloss.NewStyle().Foreground(clrGreen).Render(val))
		}
		left.WriteString(stat("Tor Address:", truncate(m.srv.Stats.TorAddress, colW-22)))
		left.WriteString(stat("Uptime:", uptime.String()))
		left.WriteString(stat("Total Connections:", fmt.Sprintf("%d", m.srv.Stats.TotalConnections)))
		left.WriteString(stat("Messages Sent:", fmt.Sprintf("%d", m.srv.Stats.MessagesSent)))
		left.WriteString(stat("Commands Executed:", fmt.Sprintf("%d", m.srv.Stats.CommandsExecuted)))
		left.WriteString("\n")
		left.WriteString(styleGray("  ── Active Clients ───────────────\n"))
		left.WriteString(fmt.Sprintf("  %-18s %s\n", "Whitelisted:",
			lipgloss.NewStyle().Foreground(clrGreen).Render(fmt.Sprintf("%d", wl))))
		left.WriteString(fmt.Sprintf("  %-18s %s\n", "Blacklisted:",
			lipgloss.NewStyle().Foreground(clrRed).Render(fmt.Sprintf("%d", bl))))
		left.WriteString(fmt.Sprintf("  %-18s %s\n", "Pending:",
			lipgloss.NewStyle().Foreground(clrYellow).Render(fmt.Sprintf("%d", pend))))
		left.WriteString("\n")
		left.WriteString(styleGray("  ── Console Log ──────────────────\n"))
		logs := m.srv.ConsoleHistory
		logStart := len(logs) - 20
		if logStart < 0 {
			logStart = 0
		}
		for _, l := range logs[logStart:] {
			left.WriteString("  " + styleGray(truncate(l, colW-4)) + "\n")
		}

		// ── RIGHT COLUMN: Live Event Feed ─────────────────────────────────────
		var right strings.Builder
		right.WriteString(styleTitle.Width(colW).Render(" 🔔 LIVE NOTIFICATIONS ") + "\n")

		events := m.srv.GetEvents()
		if len(events) == 0 {
			right.WriteString(styleGray("  No events yet.\n"))
		} else {
			// Show last events that fit, newest at bottom
			evStart := len(events) - 60
			if evStart < 0 {
				evStart = 0
			}
			for _, ev := range events[evStart:] {
				ts := styleGray(ev.Time.Format("15:04:05"))
				icon := ev.Icon
				text := lipgloss.NewStyle().Foreground(clrWhite).Render(truncate(ev.Text, colW-16))
				right.WriteString(fmt.Sprintf("  %s %s %s\n", ts, icon, text))
			}
		}

		leftStr := lipgloss.NewStyle().Width(colW).Render(left.String())
		rightStr := lipgloss.NewStyle().
			Width(colW).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(clrDkGreen).
			PaddingLeft(1).
			Render(right.String())

		return lipgloss.JoinHorizontal(lipgloss.Top, leftStr, rightStr)


	case tabWhitelist:
		var sb strings.Builder
		sb.WriteString(styleTitle.Render(" WHITELIST CHAT ") + "\n")
		for _, msg := range m.srv.DBManager.GetHistory() {
			ts := styleGray("[" + msg.Timestamp + "]")
			sender := styleWL.Render(msg.Sender)
			sb.WriteString(fmt.Sprintf("  %s %s: %s\n", ts, sender, msg.Content))
		}
		return sb.String()

	case tabBlacklist:
		var sb strings.Builder
		sb.WriteString(styleTitle.Render(" BLACKLIST CHAT (Shadow Room) ") + "\n")
		for _, msg := range m.srv.BlacklistDB.GetHistory() {
			ts := styleGray("[" + msg.Timestamp + "]")
			sender := styleBL.Render(msg.Sender)
			sb.WriteString(fmt.Sprintf("  %s %s: %s\n", ts, sender, msg.Content))
		}
		return sb.String()

	case tabClients:
		var sb strings.Builder
		sb.WriteString(styleTitle.Render(" CONNECTED CLIENTS ") + "\n")
		all := m.srv.ClientManager.GetAllClients()
		if len(all) == 0 {
			sb.WriteString(styleGray("  No clients connected.\n"))
		}
		for _, c := range all {
			stateColor := clrGray
			switch c.State {
			case "WHITELISTED":
				stateColor = clrGreen
			case "BLACKLISTED":
				stateColor = clrRed
			case "PENDING":
				stateColor = clrYellow
			}
			stateStr := lipgloss.NewStyle().Foreground(stateColor).Render(fmt.Sprintf("%-12s", c.State))
			admin := ""
			if c.IsAdmin {
				admin = lipgloss.NewStyle().Foreground(clrCyan).Render(" [ADMIN]")
			}
			sb.WriteString(fmt.Sprintf("  %-18s %s %s%s\n", c.Nickname, stateStr, c.Address, admin))
		}
		return sb.String()

	case tabFiles:
		files := m.srv.GetMediaList()

		// Sort by upload time (newest first)
		for i := 0; i < len(files)-1; i++ {
			for j := i + 1; j < len(files); j++ {
				if files[j].UploadedAt.After(files[i].UploadedAt) {
					files[i], files[j] = files[j], files[i]
				}
			}
		}

		var sb strings.Builder

		// ── Header ───────────────────────────────────────────────────────────
		if len(files) == 0 {
			sb.WriteString(styleTitle.Render(" FILES ") + "\n\n")
			// Single Render call avoids per-line width calculations that cause
			// alignment drift when emoji (multi-byte) is present in the first line.
			sb.WriteString(lipgloss.NewStyle().Foreground(clrGray).Italic(true).Render(
				"  No files uploaded yet.\n"+
					"  Files uploaded by clients are stored in RAM\n"+
					"  and appear here for download or deletion.",
			) + "\n")
			return sb.String()
		}

		// Calculate total RAM used (base64 len × 3/4 = raw bytes)
		var totalBytes int
		for _, f := range files {
			totalBytes += len(f.Data) * 3 / 4
		}
		totalStr := formatFileSize(totalBytes)

		header := fmt.Sprintf(" FILES  %s  ·  %d file(s)  ·  %s in RAM ",
			lipgloss.NewStyle().Foreground(clrGray).Render(""),
			len(files), totalStr)
		sb.WriteString(styleTitle.Render(header) + "\n")

		// ── Column header ─────────────────────────────────────────────────────
		colID := 10
		colName := 26
		colUp := 14
		colSize := 10

		hdr := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s",
			colID, "ID",
			colName, "Filename",
			colUp, "Uploader",
			colSize, "Size",
			"Uploaded",
		)
		sb.WriteString(lipgloss.NewStyle().Foreground(clrDkGreen).Bold(true).Render(hdr) + "\n")
		sb.WriteString(styleGray("  " + strings.Repeat("─", m.width-6) + "\n"))

		// ── File rows ─────────────────────────────────────────────────────────
		for i, f := range files {
			sz := formatFileSize(len(f.Data) * 3 / 4)
			age := fileAge(f.UploadedAt)

			row := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s",
				colID, f.ID,
				colName, truncate(f.Filename, colName),
				colUp, truncate(f.Uploader, colUp),
				colSize, sz,
				age,
			)

			if i == m.filesCursor {
				// highlighted row
				sb.WriteString(lipgloss.NewStyle().
					Background(clrDkGreen).
					Foreground(lipgloss.Color("#000000")).
					Bold(true).
					Render(row) + "\n")
			} else {
				// alternating row dimming
				style := lipgloss.NewStyle().Foreground(clrWhite)
				if i%2 == 1 {
					style = style.Foreground(clrGray)
				}
				sb.WriteString(style.Render(row) + "\n")
			}
		}

		// ── Detail card for selected file ─────────────────────────────────────
		if m.filesCursor < len(files) {
			sel := files[m.filesCursor]
			rawSize := len(sel.Data) * 3 / 4

			sb.WriteString("\n")
			sb.WriteString(styleGray("  " + strings.Repeat("─", m.width-6) + "\n"))

			cardW := m.width - 8
			if cardW < 40 {
				cardW = 40
			}

			detail := fmt.Sprintf(
				"  📄 %s\n"+
					"  Uploader : %s\n"+
					"  Size     : %s  (%d bytes raw)\n"+
					"  Uploaded : %s\n"+
					"  Download : /download %s <save-dir>",
				lipgloss.NewStyle().Foreground(clrGreen).Bold(true).Render(sel.Filename),
				lipgloss.NewStyle().Foreground(clrCyan).Render(sel.Uploader),
				lipgloss.NewStyle().Foreground(clrYellow).Render(formatFileSize(rawSize)),
				rawSize,
				sel.UploadedAt.Format("2006-01-02 15:04:05"),
				sel.ID,
			)

			card := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(clrDkGreen).
				Padding(0, 1).
				Width(cardW).
				Render(detail)

			sb.WriteString(card + "\n")
		}

		// ── Action bar ────────────────────────────────────────────────────────
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(clrGray).Render(
			"  [↑↓] navigate   [D/Del] delete   [Tab] switch tab\n"))

		return sb.String()

	case tabSystem:
		snap := m.sysSnap
		if snap.CollectedAt.IsZero() {
			return styleGray("  Collecting metrics...\n")
		}

		var sb strings.Builder
		sb.WriteString(styleTitle.Render(" SYSTEM MONITOR ") + "\n")

		// ── Host Info ──────────────────────────────────────────────────────────
		sb.WriteString(styleGray("  ── Host ──────────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  %-18s %s\n", "Hostname:", lipgloss.NewStyle().Foreground(clrGreen).Render(snap.Hostname)))
		sb.WriteString(fmt.Sprintf("  %-18s %s / %s\n", "Platform:", snap.OS, snap.Arch))
		sb.WriteString(fmt.Sprintf("  %-18s %s\n", "Go Version:", snap.GoVersion))
		sb.WriteString("\n")

		// ── Load ───────────────────────────────────────────────────────────────
		sb.WriteString(styleGray("  ── CPU / Load ──────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  %-18s %d\n", "CPU Cores:", snap.NumCPU))
		loadColor := clrGreen
		if snap.LoadAvg1 > float64(snap.NumCPU)*0.8 {
			loadColor = clrRed
		} else if snap.LoadAvg1 > float64(snap.NumCPU)*0.5 {
			loadColor = clrYellow
		}
		loadStr := lipgloss.NewStyle().Foreground(loadColor).Render(
			fmt.Sprintf("%.2f  %.2f  %.2f", snap.LoadAvg1, snap.LoadAvg5, snap.LoadAvg15))
		sb.WriteString(fmt.Sprintf("  %-18s %s  (1m / 5m / 15m)\n", "Load Avg:", loadStr))
		sb.WriteString("\n")

		// ── Memory ────────────────────────────────────────────────────────────
		memPct := sysmon.MemPercent(snap)
		memColor := clrGreen
		if memPct > 85 {
			memColor = clrRed
		} else if memPct > 65 {
			memColor = clrYellow
		}
		barW := 30
		bar := lipgloss.NewStyle().Foreground(memColor).Render(sysmon.ProgressBar(memPct, barW))

		sb.WriteString(styleGray("  ── Memory ────────────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  %-18s %s  %s / %s  (%.1f%%)\n",
			"RAM:",
			bar,
			sysmon.FormatKB(snap.MemUsedKB),
			sysmon.FormatKB(snap.MemTotalKB),
			memPct,
		))
		if snap.SwapTotalKB > 0 {
			swapUsed := snap.SwapTotalKB - snap.SwapFreeKB
			swapPct := float64(swapUsed) / float64(snap.SwapTotalKB) * 100
			swapBar := sysmon.ProgressBar(swapPct, barW)
			sb.WriteString(fmt.Sprintf("  %-18s %s  %s / %s  (%.1f%%)\n",
				"Swap:",
				swapBar,
				sysmon.FormatKB(swapUsed),
				sysmon.FormatKB(snap.SwapTotalKB),
				swapPct,
			))
		}
		sb.WriteString("\n")

		// ── Go Runtime ────────────────────────────────────────────────────────
		sb.WriteString(styleGray("  ── Go Runtime ───────────────────────────────────────────\n"))
		sb.WriteString(fmt.Sprintf("  %-18s %d\n", "Goroutines:", snap.Goroutines))
		sb.WriteString(fmt.Sprintf("  %-18s %.2f MB\n", "Heap Alloc:", snap.GoAllocMB))
		sb.WriteString(fmt.Sprintf("  %-18s %.2f MB\n", "Sys Memory:", snap.GoSysMB))
		sb.WriteString(fmt.Sprintf("  %-18s %d\n", "GC Cycles:", snap.GoNumGC))
		sb.WriteString("\n")

		// ── Network ───────────────────────────────────────────────────────────
		sb.WriteString(styleGray("  ── Network ─────────────────────────────────────────────\n"))
		if len(snap.NetIfaces) == 0 {
			sb.WriteString(styleGray("  No network interfaces detected.\n"))
		}
		for _, iface := range snap.NetIfaces {
			sb.WriteString(fmt.Sprintf("  %-10s  ↓ %-14s  ↑ %s\n",
				iface.Name,
				sysmon.FormatBytes(iface.RxBytes),
				sysmon.FormatBytes(iface.TxBytes),
			))
		}
		sb.WriteString(fmt.Sprintf("  %-18s ESTABLISHED:%d  LISTEN:%d  TIME_WAIT:%d\n",
			"TCP:",
			snap.TCPEstablished,
			snap.TCPListening,
			snap.TCPTimeWait,
		))
		sb.WriteString("\n")

		// ── Services ──────────────────────────────────────────────────────────
		sb.WriteString(styleGray("  ── Services ────────────────────────────────────────────\n"))
		torStatus := lipgloss.NewStyle().Foreground(clrRed).Render("● STOPPED")
		if snap.TorRunning {
			torStatus = lipgloss.NewStyle().Foreground(clrGreen).Render("● RUNNING")
		}
		nginxStatus := lipgloss.NewStyle().Foreground(clrRed).Render("● STOPPED")
		if snap.NginxRunning {
			nginxStatus = lipgloss.NewStyle().Foreground(clrGreen).Render("● RUNNING")
		}
		sb.WriteString(fmt.Sprintf("  %-18s %s\n", "Tor:", torStatus))
		sb.WriteString(fmt.Sprintf("  %-18s %s\n", "NGINX:", nginxStatus))
		sb.WriteString(fmt.Sprintf("  %-18s %s\n", "Conner Server:", lipgloss.NewStyle().Foreground(clrGreen).Render("● RUNNING")))
		sb.WriteString("\n")
		sb.WriteString(styleGray(fmt.Sprintf("  Last updated: %s\n", snap.CollectedAt.Format("15:04:05"))))
		return sb.String()
	}
	return ""
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func styleGray(s string) string {
	return lipgloss.NewStyle().Foreground(clrGray).Render(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// formatFileSize returns a human-readable file size string.
func formatFileSize(bytes int) string {
	switch {
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1024/1024)
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// fileAge returns a compact relative time string (e.g. "2m ago", "1h ago").
func fileAge(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("01-02 15:04")
	}
}
