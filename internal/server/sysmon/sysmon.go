package sysmon

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Snapshot holds a point-in-time view of all system metrics.
type Snapshot struct {
	CollectedAt time.Time

	// System
	Hostname    string
	OS          string
	Arch        string
	Goroutines  int
	GoVersion   string

	// CPU
	LoadAvg1  float64
	LoadAvg5  float64
	LoadAvg15 float64
	NumCPU    int

	// Memory
	MemTotalKB     uint64
	MemAvailableKB uint64
	MemUsedKB      uint64
	MemCachedKB    uint64
	SwapTotalKB    uint64
	SwapFreeKB     uint64

	// Go Heap
	GoAllocMB  float64
	GoSysMB    float64
	GoNumGC    uint32

	// Network
	NetIfaces []NetIface

	// TCP
	TCPEstablished int
	TCPListening   int
	TCPTimeWait    int

	// Services
	TorRunning   bool
	NginxRunning bool
}

// NetIface is per-interface RX/TX counters.
type NetIface struct {
	Name   string
}

// Collect reads all metrics that are accessible without elevated privileges.
func Collect() Snapshot {
	s := Snapshot{
		CollectedAt: time.Now(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		GoVersion:   runtime.Version(),
		NumCPU:      runtime.NumCPU(),
		Goroutines:  runtime.NumGoroutine(),
	}

	// Hostname
	if h, err := os.Hostname(); err == nil {
		s.Hostname = h
	} else {
		s.Hostname = "unknown"
	}

	// Load average  (/proc/loadavg)
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			s.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
			s.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
			s.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// Memory  (/proc/meminfo)
	if f, err := os.Open("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			val, _ := strconv.ParseUint(parts[1], 10, 64)
			switch parts[0] {
			case "MemTotal:":
				s.MemTotalKB = val
			case "MemAvailable:":
				s.MemAvailableKB = val
			case "Cached:":
				s.MemCachedKB = val
			case "SwapTotal:":
				s.SwapTotalKB = val
			case "SwapFree:":
				s.SwapFreeKB = val
			}
		}
		f.Close()
		if s.MemTotalKB > 0 {
			s.MemUsedKB = s.MemTotalKB - s.MemAvailableKB
		}
	}

	// Go runtime heap stats
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.GoAllocMB = float64(ms.Alloc) / (1024 * 1024)
	s.GoSysMB = float64(ms.Sys) / (1024 * 1024)
	s.GoNumGC = ms.NumGC

	// Network interfaces  (/proc/net/dev)
	if f, err := os.Open("/proc/net/dev"); err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Scan() // header line 1
		scanner.Scan() // header line 2
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			colonIdx := strings.Index(line, ":")
			if colonIdx < 0 {
				continue
			}
			name := strings.TrimSpace(line[:colonIdx])
			if name == "lo" {
				continue // skip loopback
			}
			fields := strings.Fields(line[colonIdx+1:])
			if len(fields) < 9 {
				continue
			}
			_, _ = strconv.ParseUint(fields[0], 10, 64)
			_, _ = strconv.ParseUint(fields[8], 10, 64)
			s.NetIfaces = append(s.NetIfaces, NetIface{
				Name:    name,
			})
		}
		f.Close()
	}

	// TCP connection states  (/proc/net/tcp)
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if f, err := os.Open(path); err == nil {
			scanner := bufio.NewScanner(f)
			scanner.Scan() // skip header
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) < 4 {
					continue
				}
				state := fields[3]
				switch state {
				case "01": // ESTABLISHED
					s.TCPEstablished++
				case "0A": // LISTEN
					s.TCPListening++
				case "06": // TIME_WAIT
					s.TCPTimeWait++
				}
			}
			f.Close()
		}
	}

	// Service detection via /proc scan (no pidof/pgrep required)
	s.TorRunning = procRunning("tor")
	s.NginxRunning = procRunning("nginx")

	return s
}

// procRunning scans /proc for a process whose cmdline contains name.
func procRunning(name string) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Only numeric entries are PIDs
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(cmdline)) == name {
			return true
		}
	}
	return false
}

// ─── Formatting helpers ───────────────────────────────────────────────────────

func FormatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func FormatKB(kb uint64) string {
	return FormatBytes(kb * 1024)
}

// MemPercent returns used RAM as a percentage string.
func MemPercent(s Snapshot) float64 {
	if s.MemTotalKB == 0 {
		return 0
	}
	return math.Round(float64(s.MemUsedKB)/float64(s.MemTotalKB)*100*10) / 10
}

// ProgressBar renders a simple ASCII bar for a 0–100 value.
func ProgressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
