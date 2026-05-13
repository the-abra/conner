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
	stealth := flag.Bool("stealth", false, "Enable anti-forensics and stealth mode")
	
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage (Client): %s [options] <nickname> [address:port]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage (Server): %s --server [options]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := autoSetup(*isServer); err != nil {
		fmt.Printf("[!] Auto-setup warning: %v\n", err)
	}

	if err := startTor(*isServer); err != nil {
		log.Fatalf("[!] Tor initialization failed: %v", err)
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
		ports = append(ports, config.ServerPort)
	} else {
		ports = append(ports, "8888") // P2P Hosting port
	}
	for _, p := range ports {
		if err := checkPortAvailability(p); err != nil {
			log.Fatalf("[!] Port Conflict: %v. Please ensure port %s is free.", err, p)
		}
	}

	if *isServer {
		if serverOnion != "" {
			fmt.Println("\n" + strings.Repeat("=", 60))
			fmt.Println("  CONNER SERVER IS READY")
			fmt.Println(strings.Repeat("=", 60))
			fmt.Printf("  YOUR ONION ADDRESS: %s\n", serverOnion)
			fmt.Println(strings.Repeat("=", 60))
			fmt.Println("  (You can copy the address now)")
			fmt.Print("  Press [ENTER] to launch the Admin Dashboard...")
			fmt.Scanln()
		}
		runServer()
	} else {
		runClient()
	}

	if embeddedTor != nil {
		embeddedTor.Stop()
	}
}

func runServer() {
	srv := server.NewServer()
	if serverOnion != "" {
		srv.Stats.TorAddress = serverOnion
	}

	startErr := make(chan error, 1)
	go func() {
		startErr <- srv.Start(config.ServerPort)
	}()

	// Wait a moment for potential bind errors
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-startErr:
		log.SetOutput(os.Stderr)
		log.Fatalf("Server failed to start: %v", err)
	default:
		// Server seems up
	}

	log.SetOutput(io.Discard)

	p := tea.NewProgram(servertui.InitialModel(srv), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.SetOutput(os.Stderr)
		log.Fatal(err)
	}
}

func runClient() {
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

	cli, err := client.Connect(nickname, address)
	if err != nil {
		if err == client.ErrBanned {
			p := tea.NewProgram(clienttui.InitialModel(nil, nickname), tea.WithAltScreen(), tea.WithMouseCellMotion())
			if _, err := p.Run(); err != nil {
				log.Fatal(err)
			}
			return
		}
		log.Fatalf("Failed to connect: %v", err)
	}

	cli.StartAutoSync()

	p := tea.NewProgram(clienttui.InitialModel(cli, nickname), tea.WithAltScreen(), tea.WithMouseCellMotion())
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

func startTor(isServer bool) error {
	// Always write the latest local torrc
	fmt.Println("[*] Configuring Local Tor Instance...")
	torrc := config.GetTorrcTemplate(isServer)
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
		fmt.Println("[*] Generating Ephemeral Hidden Service...")
		onion, err := et.CreateHiddenService(context.Background(), 6666, 80)
		if err != nil {
			return fmt.Errorf("failed to create hidden service: %w", err)
		}
		serverOnion = onion
	}

	fmt.Println("[+] Tor is UP. Address: " + serverOnion)
	sysmon.SetTorStatus(true)
	return nil
}

func enableStealth(isServer bool) error {
	if isServer && os.Geteuid() != 0 {
		return fmt.Errorf("stealth mode requires root")
	}

	if isServer {
		fmt.Println("[*] Activating Anti-Forensics Shield...")
		os.Setenv("HISTSIZE", "0")
		os.Setenv("HISTFILE", "/dev/null")
		runCmd("sh", "-c", "echo 'export HISTSIZE=0' >> /etc/profile")
		runCmd("sh", "-c", "echo 'export HISTFILE=/dev/null' >> /etc/profile")

		logs := []string{
			"/var/log/messages", "/var/log/syslog", "/var/log/auth.log",
			"/var/log/nginx/access.log", "/var/log/nginx/error.log",
		}
		for _, l := range logs {
			runCmd("shred", "-u", l)
			os.MkdirAll(filepath.Dir(l), 0755)
			os.Remove(l)
			os.Symlink("/dev/null", l)
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
	} else {
		os.Setenv("HISTSIZE", "0")
		os.Setenv("HISTFILE", "/dev/null")
		home := os.Getenv("HOME")
		_ = exec.Command("shred", "-u", home+"/.bash_history").Run()
		_ = exec.Command("shred", "-u", home+"/.sh_history").Run()
	}

	return nil
}

func checkPortAvailability(port string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return fmt.Errorf("port %s is already in use", port)
	}
	_ = ln.Close()
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
