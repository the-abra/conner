package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"conner/internal/client"
	clienttui "conner/internal/client/tui"
	"conner/internal/config"
	"conner/internal/server"
	"conner/internal/server/sysmon"
	servertui "conner/internal/server/tui"
	"conner/internal/tor"

	tea "github.com/charmbracelet/bubbletea"
	"context"
	"net"
)

var embeddedTor *tor.EmbeddedTor
var serverOnion string

func main() {
	isServer := flag.Bool("server", false, "Run in server mode")
	useTor := flag.Bool("tor", false, "Enable Tor integration (Onion service / SOCKS proxy)")
	stealth := flag.Bool("stealth", false, "Enable anti-forensics and stealth mode")
	srvPort := flag.String("port", "6666", "Server port to listen on (default 6666)")
	flag.StringVar(srvPort, "p", "6666", "Server port to listen on (shorthand)")
	forceStealth := flag.Bool("force-system-stealth", false, "Force aggressive system-wide forensic wiping (requires root)")
	autoApprove := flag.Bool("auto-approve", false, "Automatically approve all incoming connections (Server Mode)")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage (Client): %s [options] <nickname> [address:port]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage (Server): %s --server [options]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := autoSetup(*isServer); err != nil {
		fmt.Printf("[!] Auto-setup warning: %v\n", err)
	}

	// Auto-enable/disable Tor for client based on target address
	if !*isServer {
		args := flag.Args()
		address := "127.0.0.1:6666"
		if len(args) >= 2 {
			address = args[1]
		}
		
		if strings.Contains(address, ".onion") {
			if !*useTor {
				fmt.Println("[*] Target address is an onion address. Auto-enabling Tor.")
				*useTor = true
			}
		} else if *useTor {
			fmt.Println("[*] Target address is not an onion address. Auto-disabling Tor.")
			*useTor = false
		}
	}

	if *useTor {
		if err := startTor(*isServer, *srvPort); err != nil {
			log.Fatalf("[!] Tor initialization failed: %v", err)
		}
	} else {
		fmt.Println("[*] Tor is disabled. Running in direct-connection mode.")
	}

	if *stealth {
		if os.Geteuid() != 0 {
			log.Fatalf("[!] CRITICAL: Stealth mode requires root privileges (sudo).")
		}
		if err := enableStealth(*isServer); err != nil {
			fmt.Printf("[!] Stealth mode activation failed: %v\n", err)
		}
	}

	// Port sanity checks
	var ports []string
	if *isServer {
		ports = append(ports, *srvPort)
	} else {
		ports = append(ports, "8888") // Local P2P Port
	}
	for _, p := range ports {
		if err := checkPortAvailability(p); err != nil {
			log.Fatalf("[!] Port Conflict: %v. Please ensure port %s is free.", err, p)
		}
	}

	if *isServer {
		runServer(*srvPort, *stealth, *forceStealth, *autoApprove)
	} else {
		runClient(*useTor, *stealth)
	}

	if embeddedTor != nil {
		embeddedTor.Stop()
	}
}

func runServer(port string, stealth bool, forceStealth bool, autoApprove bool) {
	srv := server.NewServer()
	srv.AutoApprove = autoApprove
	
	startErr := make(chan error, 1)
	go func() {
		startErr <- srv.Start(port)
	}()

	// Wait for server to start and bind its HTTP port
	select {
	case <-srv.Ready:
	case <-time.After(10 * time.Second):
		log.Fatalf("[!] Timeout waiting for server to start")
	}

	if embeddedTor != nil {
		fmt.Println("[*] Generating Multi-Port Server Onion...")
		pInt := 6666
		fmt.Sscanf(port, "%d", &pInt)
		onion, err := embeddedTor.CreateServerOnion(context.Background(), pInt, srv.HTTPPort)
		if err != nil {
			log.Fatalf("Failed to create multi-port onion: %v", err)
		}
		srv.Stats.TorAddress = onion
		serverOnion = onion
	} else if serverOnion != "" {
		srv.Stats.TorAddress = serverOnion
	}

	if serverOnion != "" {
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("  CONNER SERVER IS READY (TOR MODE)")
		fmt.Println(strings.Repeat("=", 60))
		fmt.Printf("  YOUR ONION ADDRESS: %s:%s\n", serverOnion, port)
		fmt.Println(strings.Repeat("=", 60))
		fmt.Println("  (You can copy the address now)")
		fmt.Print("  Press [ENTER] to launch the Admin Dashboard...")
		fmt.Scanln()
	} else {
		srv.Stats.TorAddress = "127.0.0.1" // Default for local
		fmt.Println("\n" + strings.Repeat("=", 60))
		fmt.Println("  CONNER SERVER IS READY (DIRECT MODE)")
		fmt.Println(strings.Repeat("=", 60))
		fmt.Printf("  LISTENING ON: 0.0.0.0:%s\n", port)
		fmt.Println(strings.Repeat("=", 60))
		fmt.Print("  Press [ENTER] to launch the Admin Dashboard...")
		fmt.Scanln()
	}

	if stealth {
		if forceStealth {
			if err := enableSystemStealth(); err != nil {
				fmt.Printf("[!] System-wide stealth failed: %v\n", err)
			}
		}
		log.SetOutput(io.Discard)
	}

	log.SetOutput(io.Discard) // Prevent leakage to terminal
	p := tea.NewProgram(servertui.InitialModel(srv), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatal(err)
	}
}

func runClient(useTor bool, stealth bool) {
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	nickname := args[0]
	address := "127.0.0.1:6666" // default
	if len(args) >= 2 {
		address = args[1]
	}

	// AUTO-PORT: If no port is specified, default to 6666
	if address != "" && !strings.Contains(address, ":") {
		address = address + ":6666"
	}

	cli, err := client.Connect(nickname, address, useTor, embeddedTor)
	if err != nil {
		if err == client.ErrBanned {
			p := tea.NewProgram(clienttui.InitialModel(nil, nickname, address, useTor, embeddedTor), tea.WithAltScreen(), tea.WithMouseCellMotion())
			if _, err := p.Run(); err != nil {
				log.Fatal(err)
			}
			return
		}
		log.Fatalf("Failed to connect: %v", err)
	}

	cli.StartAutoSync()

	if stealth {
		log.SetOutput(io.Discard)
	}

	p := tea.NewProgram(clienttui.InitialModel(cli, nickname, address, useTor, embeddedTor), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}


func autoSetup(isServer bool) error {
	optionalTools := []string{}
	if isServer {
		optionalTools = append(optionalTools, "shred")
	} else {
		optionalTools = append(optionalTools, "shred", "img2sixel")
	}

	var missing []string
	for _, tool := range optionalTools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}

	for _, tool := range missing {
		fmt.Printf("[!] Optional tool '%s' is missing. Some features may be limited.\n", tool)
	}

	return nil
}

func startTor(isServer bool, srvPort string) error {
	// Always write the latest local torrc
	fmt.Println("[*] Configuring Local Tor Instance...")
	torrc := config.GetTorrcTemplate(isServer, srvPort, "6667")
	_ = os.WriteFile(config.TorrcPath, []byte(torrc), 0600)

	dir := filepath.Join(config.TorDataDir, "conner_chat")
	if !isServer {
		dir = filepath.Join(config.TorDataDir, "conner_client")
	}
	_ = os.MkdirAll(dir, 0700)

	// Try system tor first (faster)
	if _, err := exec.LookPath("tor"); err == nil {
		fmt.Println("[*] Starting Local Tor process...")
		_ = exec.Command("pkill", "-f", config.TorrcPath).Run()
		time.Sleep(500 * time.Millisecond)

		cmd := exec.Command("tor", "-f", config.TorrcPath, "--RunAsDaemon", "1")
		if err := cmd.Run(); err == nil {
			if isServer {
				// Wait for hostname to be generated
				hostnamePath := filepath.Join(config.TorDataDir, "conner_chat", "hostname")
				for i := 0; i < 10; i++ {
					if b, err := os.ReadFile(hostnamePath); err == nil {
						serverOnion = strings.TrimSpace(string(b))
						break
					}
					time.Sleep(1 * time.Second)
				}
			}

			sysmon.SetTorStatus(true)
			return nil
		}
		fmt.Println("[!] Failed to start system Tor. Falling back to embedded Tor...")
	}

	fmt.Println("[*] Starting EMBEDDED Tor motor (This might take a few seconds)...")
	et, err := tor.StartEmbedded(context.Background())
	if err != nil {
		return fmt.Errorf("failed to start any tor instance: %w", err)
	}
	embeddedTor = et
	config.TorSocksAddr = et.SocksAddr

	if isServer {
		// Onion creation moved to runServer to wait for srv.HTTPPort
	}

	fmt.Println("[+] Tor is UP. Address: " + serverOnion)
	sysmon.SetTorStatus(true)
	return nil
}

func enableStealth(isServer bool) error {
	if isServer {
		fmt.Println("[*] Activating Session-Level Privacy...")
		os.Setenv("HISTSIZE", "0")
		os.Setenv("HISTFILE", "/dev/null")
	} else {
		os.Setenv("HISTSIZE", "0")
		os.Setenv("HISTFILE", "/dev/null")
		home := os.Getenv("HOME")
		_ = exec.Command("shred", "-u", home+"/.bash_history").Run()
		_ = exec.Command("shred", "-u", home+"/.sh_history").Run()
	}

	return nil
}

func enableSystemStealth() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("system-wide stealth requires root")
	}

	fmt.Println("[*] Activating Anti-Forensics Shield...")
	runCmd("sh", "-c", "echo 'export HISTSIZE=0' >> /etc/profile")
	runCmd("sh", "-c", "echo 'export HISTFILE=/dev/null' >> /etc/profile")

	fmt.Println("[*] Wiping system journals and forensic records...")
	runCmd("journalctl", "--vacuum-time=1s")
	runCmd("dmesg", "-C")
	runCmd("truncate", "-s", "0", "/var/log/wtmp")
	runCmd("truncate", "-s", "0", "/var/log/btmp")
	runCmd("truncate", "-s", "0", "/var/log/lastlog")
	runCmd("auditctl", "-D")

	logs := []string{
		"/var/log/messages", "/var/log/syslog", "/var/log/auth.log",
		"/var/log/nginx/access.log", "/var/log/nginx/error.log",
		"/var/lib/systemd/coredump/*",
	}
	for _, l := range logs {
		if _, err := os.Stat(l); err == nil {
			runCmd("shred", "-u", l)
			os.Symlink("/dev/null", l)
		}
	}

	fmt.Println("[*] Hardening firewall (DDoS mitigation)...")
	runCmd("iptables", "-F")
	runCmd("iptables", "-A", "INPUT", "-m", "conntrack", "--ctstate", "INVALID", "-j", "DROP")
	runCmd("iptables", "-A", "INPUT", "-p", "tcp", "--tcp-flags", "ALL", "ALL", "-j", "DROP")
	runCmd("iptables", "-A", "INPUT", "-p", "tcp", "--dport", "6666", "-m", "connlimit", "--connlimit-above", "20", "-j", "REJECT")
	runCmd("iptables", "-A", "INPUT", "-i", "lo", "-j", "ACCEPT")
	runCmd("iptables", "-A", "INPUT", "-p", "tcp", "--dport", "80", "-j", "ACCEPT")
	runCmd("iptables", "-A", "INPUT", "-p", "tcp", "--dport", "6666", "-j", "ACCEPT")
	runCmd("iptables", "-P", "INPUT", "DROP")

	fmt.Println("[*] Mounting RAM disks...")
	runCmd("mount", "-t", "tmpfs", "-o", "size=128M,noexec,nosuid,nodev", "tmpfs", "/tmp")
	runCmd("mount", "-t", "tmpfs", "-o", "size=64M,noexec,nosuid,nodev", "tmpfs", "/var/log")
	runCmd("mount", "-o", "remount,hidepid=2", "/proc")

	return nil
}

func checkPortAvailability(port string) error {
	// Check on both 127.0.0.1 and 0.0.0.0
	for _, addr := range []string{"127.0.0.1", "0.0.0.0"} {
		ln, err := net.Listen("tcp", addr+":"+port)
		if err != nil {
			return fmt.Errorf("port %s is already in use on %s", port, addr)
		}
		_ = ln.Close()
	}
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
