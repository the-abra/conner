package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"conner/internal/client"
	clienttui "conner/internal/client/tui"
	"conner/internal/config"
	"conner/internal/server"
	servertui "conner/internal/server/tui"

	tea "github.com/charmbracelet/bubbletea"
)

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

	if *stealth {
		if err := enableStealth(*isServer); err != nil {
			fmt.Printf("[!] Stealth mode activation failed: %v\n", err)
		}
	}

	if *isServer {
		runServer()
	} else {
		runClient()
	}
}

func runServer() {
	srv := server.NewServer()

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

func detectPackageManager() string {
	if _, err := exec.LookPath("apk"); err == nil {
		return "apk"
	}
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "apt"
	}
	if _, err := exec.LookPath("pacman"); err == nil {
		return "pacman"
	}
	return ""
}

func autoSetup(isServer bool) error {
	requiredTools := []string{"tor"}
	if isServer {
		requiredTools = append(requiredTools, "nginx", "iptables", "shred")
	} else {
		requiredTools = append(requiredTools, "shred", "img2sixel")
	}

	var missing []string
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}

	if len(missing) > 0 {
		pm := detectPackageManager()
		isRoot := os.Geteuid() == 0

		if !isRoot {
			fmt.Printf("[!] Missing dependencies: %s\n", strings.Join(missing, ", "))
			fmt.Println("[!] Please run as root or install them manually.")
			os.Exit(1)
		}

		fmt.Printf("[*] Missing dependencies: %s. Attempting to install via %s...\n", strings.Join(missing, ", "), pm)

		pkgMap := map[string]map[string][]string{
			"apk": {
				"tor":       {"tor"},
				"nginx":     {"nginx", "nginx-mod-stream"},
				"iptables":  {"iptables"},
				"shred":     {"coreutils"},
				"img2sixel": {"libsixel-tools"},
			},
			"apt": {
				"tor":       {"tor"},
				"nginx":     {"nginx", "libnginx-mod-stream"},
				"iptables":  {"iptables"},
				"shred":     {"coreutils"},
				"img2sixel": {"libsixel-bin"},
			},
			"pacman": {
				"tor":       {"tor"},
				"nginx":     {"nginx"},
				"iptables":  {"iptables"},
				"shred":     {"coreutils"},
				"img2sixel": {"libsixel"},
			},
		}

		var pkgsToInstall []string
		for _, tool := range missing {
			if p, ok := pkgMap[pm][tool]; ok {
				pkgsToInstall = append(pkgsToInstall, p...)
			}
		}

		if len(pkgsToInstall) > 0 {
			var err error
			switch pm {
			case "apk":
				runCmd("apk", "update")
				err = runCmd("apk", append([]string{"add", "--no-cache", "libc6-compat"}, pkgsToInstall...)...)
			case "apt":
				runCmd("apt-get", "update")
				err = runCmd("apt-get", append([]string{"install", "-y"}, pkgsToInstall...)...)
			case "pacman":
				err = runCmd("pacman", append([]string{"-Sy", "--noconfirm"}, pkgsToInstall...)...)
			default:
				fmt.Printf("[!] Unknown package manager. Please install manually: %s\n", strings.Join(pkgsToInstall, ", "))
				os.Exit(1)
			}

			if err != nil {
				fmt.Printf("[!] Failed to install dependencies: %v\n", err)
				fmt.Printf("[!] Manual installation required: %s\n", strings.Join(pkgsToInstall, ", "))
				os.Exit(1)
			}
		}

		for _, tool := range missing {
			if _, err := exec.LookPath(tool); err != nil {
				fmt.Printf("[!] Tool %s still missing after installation attempt.\n", tool)
				os.Exit(1)
			}
		}
	}

	if isServer {
		if _, err := os.Stat("/usr/local/bin/conner-shell"); os.IsNotExist(err) {
			fmt.Println("[*] Creating restricted shell environment...")
			shellScript := `#!/bin/sh
WORKDIR="${CONNER_WORKDIR:-/workspace}"
cd "$WORKDIR" 2>/dev/null || cd /tmp
SERVER_BIN="${WORKDIR}/conner"
if [ -z "$1" ]; then
    echo "===================================================="
    echo "       CONNER SHIELDED EXECUTION ENVIRONMENT"
    echo "===================================================="
    while true; do
        printf "conner@shielded:~$ "
        if ! read -r input; then exit 0; fi
        case "$input" in
            "start-server") exec "$SERVER_BIN" --server ;;
            "show-onion") cat hostname 2>/dev/null || echo "[-] Not generated yet." ;;
            "exit"|"quit") exit 0 ;;
            "") continue ;;
            *) echo "[-] Access Denied." ;;
        esac
    done
else
    if [ "$1" = "-c" ] && [ "$2" = "start-server" ]; then exec "$SERVER_BIN" --server; fi
    echo "[-] Access Denied."
    exit 1
fi
`
			_ = os.WriteFile("/usr/local/bin/conner-shell", []byte(shellScript), 0755)
			runCmd("sh", "-c", "grep -qxF '/usr/local/bin/conner-shell' /etc/shells || echo '/usr/local/bin/conner-shell' >> /etc/shells")
		}

		runCmd("id", "-u", "conner") 
		runCmd("adduser", "-D", "-s", "/usr/local/bin/conner-shell", "conner")

		nginxConfPath := "/etc/nginx/nginx.conf"
		needsUpdate := false
		if _, err := os.Stat(nginxConfPath); os.IsNotExist(err) {
			needsUpdate = true
		} else if b, err := os.ReadFile(nginxConfPath); err == nil {
			if strings.Contains(string(b), "stream {") && !strings.Contains(string(b), "ngx_stream_module.so") {
				// Likely missing the module load on Alpine
				needsUpdate = true
			}
		}

		if needsUpdate {
			fmt.Println("[*] Updating NGINX stealth proxy configuration...")
			// On Alpine, we must explicitly load the stream module if using nginx-mod-stream
			var loadModule string
			if _, err := os.Stat("/usr/lib/nginx/modules/ngx_stream_module.so"); err == nil {
				loadModule = "load_module /usr/lib/nginx/modules/ngx_stream_module.so;\n"
			}
			
			nginxConf := loadModule + `worker_processes auto;
error_log /tmp/nginx_error.log info;
pid /tmp/nginx.pid;
events { worker_connections 1024; }
stream {
    upstream conner_backend {
        server 127.0.0.1:6666;
    }
    server {
        listen 80;
        proxy_pass conner_backend;
        proxy_connect_timeout 10s;
    }
}`
			_ = os.WriteFile("/etc/nginx/nginx.conf", []byte(nginxConf), 0644)
		}

		if _, err := os.Stat("/etc/tor/torrc"); os.IsNotExist(err) {
			fmt.Println("[*] Configuring Tor Hidden Service...")
			torrc := `User tor
DataDirectory /var/lib/tor
Log err file /dev/null
SocksPort 127.0.0.1:9050
ControlPort 127.0.0.1:9051
CookieAuthentication 1
HiddenServiceDir /var/lib/tor/conner_chat/
HiddenServicePort 80 127.0.0.1:6666
`
			_ = os.WriteFile("/etc/tor/torrc", []byte(torrc), 0644)
			os.MkdirAll("/var/lib/tor/conner_chat", 0700)
			runCmd("chown", "-R", "tor:tor", "/var/lib/tor")
			runCmd("chmod", "0755", "/var/lib/tor")
			runCmd("chmod", "0644", "/var/lib/tor/control_auth_cookie")
		}

		if _, err := exec.LookPath("rc-service"); err == nil {
			fmt.Println("[*] Restarting services via rc-service...")
			runCmd("rc-service", "nginx", "restart")
			runCmd("rc-service", "tor", "restart")
		} else {
			fmt.Println("[*] Restarting services manually...")
			runCmd("pkill", "-9", "nginx")
			runCmd("pkill", "-9", "tor")
			time.Sleep(1 * time.Second)
			
			// Start with explicit config paths
			_ = exec.Command("nginx", "-c", "/etc/nginx/nginx.conf").Start()
			_ = exec.Command("tor", "-f", "/etc/tor/torrc", "--RunAsDaemon", "1").Start()
		}

		// Validation loop
		fmt.Println("[*] Validating proxy chain...")
		for i := 0; i < 5; i++ {
			time.Sleep(1 * time.Second)
			// Check if NGINX is listening on 80
			conn, err := net.DialTimeout("tcp", "0.0.0.0:80", 500*time.Millisecond)
			if err == nil {
				conn.Close()
				fmt.Println("[+] NGINX is UP and listening on port 80.")
				break
			}
			if i == 4 {
				fmt.Println("[!] WARNING: NGINX failed to start on port 80. Check /tmp/nginx_error.log")
			}
		}

	} else {
		// Client Setup
		if _, err := os.Stat("/etc/tor/torrc"); os.IsNotExist(err) {
			fmt.Println("[*] Configuring Tor (SOCKS5 + P2P Hidden Service)...")
			torrc := `User tor
DataDirectory /var/lib/tor
Log err file /dev/null
SocksPort 127.0.0.1:9050
ControlPort 127.0.0.1:9051
CookieAuthentication 1
HiddenServiceDir /var/lib/tor/conner_client/
HiddenServicePort 80 127.0.0.1:8888
`
			_ = os.WriteFile("/etc/tor/torrc", []byte(torrc), 0644)
			os.MkdirAll("/var/lib/tor/conner_client", 0700)
			runCmd("chown", "-R", "tor:tor", "/var/lib/tor")
		}

		if _, err := exec.LookPath("rc-service"); err == nil {
			runCmd("rc-service", "tor", "restart")
			exec.Command("tor", "--RunAsDaemon", "1").Run()
		}
	}

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

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
