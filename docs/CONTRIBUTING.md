# Contributing to CONNER

Thank you for your interest in contributing! CONNER is a security-first, high-performance anonymous chat platform written in Go.

## 1. Development Principles

When contributing to CONNER, please keep these core principles in mind:
- **Zero Disk I/O**: Never write sensitive data (messages, user lists, media) to the disk. Everything must live in RAM.
- **Memory Safety**: Use Go's concurrency primitives (`channels`, `mutexes`) correctly to avoid race conditions.
- **Minimal Footprint**: Keep binary sizes small and dependencies minimal.
- **Anti-Forensics**: Ensure no trace of the user's activity is left behind in logs or shell history.

## 2. Technical Stack

- **Language**: Go 1.21+
- **TUI Framework**: [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- **Styling**: [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- **Encryption**: X25519 (Key Exchange) + AES-GCM (Payload)
- **Networking**: Framed TCP (Big-Endian 4-byte length prefix)

## 3. How to Contribute

### Reporting Bugs
- Describe the bug clearly with steps to reproduce.
- Include information about your environment (Alpine version, Go version).
- Check if the bug is a security vulnerability. If so, report it privately.

### Code Contributions

1. **Fork the repository** and create a feature branch.
2. **Follow Go standards**:
   - Run `go fmt ./...` before committing.
   - Run `go vet ./...` and `gosec ./...` to check for common issues.
   - **Pro Tip**: Use [laction](https://github.com/the-abra/local-actions) to run all CI checks locally. Simply run `laction .` to build and vet, or `laction . test` for the race detector.
3. **TUI Changes**:
   - Ensure components are responsive to terminal resizing (`tea.WindowSizeMsg`).
   - Use the defined color palette in `internal/client/tui/app.go`.
4. **Protocol Changes**:
   - Maintain backward compatibility in `internal/protocol/protocol.go`.
   - Ensure all new message types are documented in the README.

### Commit Message Guidelines

- Use conventional commits: `feat:`, `fix:`, `docs:`, `security:`, `refactor:`.
- Example: `fix(security): sanitize file paths in media extraction`

## 4. Security Submissions

If you discover a security vulnerability, please do **not** open a public issue. Instead, provide a detailed report of the exploit and a proposed fix.

Thank you for helping keep CONNER secure and anonymous!