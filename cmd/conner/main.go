package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"conner/internal/appdir"
	"conner/internal/client"
	clienttui "conner/internal/client/tui"
	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/invite"
	"conner/internal/logx"
	"conner/internal/server"
	servertui "conner/internal/server/tui"
	"conner/internal/tor"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if err := appdir.Ensure(); err != nil {
		return err
	}

	fs := flag.NewFlagSet("conner", flag.ExitOnError)
	asServer := fs.Bool("server", false, "run hub")
	useTor := fs.Bool("tor", false, "use Tor (WAN default for hub)")
	autoApprove := fs.Bool("auto-approve", false, "auto-whitelist (LAN/lab only)")
	lan := fs.Bool("lan", false, "bind 0.0.0.0 (clearnet/LAN); default hub bind is 127.0.0.1")
	pt := fs.String("pt", "", "system-tor: use SOCKS 127.0.0.1:9050 (configure Snowflake in your torrc)")
	pass := fs.String("passphrase", "", "wrap a marker in ~/.conner/identity/master.wrap")
	port := fs.String("port", config.ServerPort, "hub listen port")
	noTUI := fs.Bool("no-tui", false, "hub logs only")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	args := fs.Args()

	if *asServer {
		logx.Info("start hub", "tor", *useTor, "lan", *lan, "pt", *pt)
		return runServer(*useTor, *autoApprove, *lan, *pt, *port, *noTUI)
	}

	nick := ""
	target := ""
	if len(args) >= 1 {
		nick = args[0]
	}
	if len(args) >= 2 {
		target = args[1]
	}
	if nick == "" {
		usage()
		return fmt.Errorf("nickname required")
	}
	if target == "" {
		usage()
		return fmt.Errorf("invite or host:port required")
	}

	blob, err := invite.Decode(target)
	if err != nil {
		return err
	}
	if *pt != "" {
		blob.PT = *pt
	}
	wantTor := *useTor || blob.Tor || strings.Contains(blob.DialAddr(), ".onion")

	var et *tor.EmbeddedTor
	if wantTor && blob.PT != "system-tor" {
		ctx := context.Background()
		et, err = tor.StartEmbedded(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "embedded Tor failed (%v); trying system tor SOCKS\n", err)
			et = nil
		}
	}

	if *pass != "" {
		_ = os.Setenv("CONNER_PASSPHRASE", *pass)
		_ = crypto.WriteWrapped(appdir.WrappedKeyFile(), *pass, []byte("conner-v3"))
	}

	cli, err := client.Connect(nick, blob.DialAddr(), wantTor, et)
	if err != nil {
		return err
	}
	room := blob.Room
	if room == "" {
		room = config.DefaultRoom
	}
	cli.SetRoomDirs(room)
	cli.StartAutoSync()

	m := clienttui.InitialModel(cli, nick, blob.DialAddr(), wantTor, et)
	// No mouse capture: the terminal keeps native Shift-select / copy.
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	if cli.Cancel != nil {
		cli.Cancel()
	}
	if et != nil {
		et.Stop()
	}
	return err
}

func runServer(useTor, autoApprove, lan bool, pt, port string, noTUI bool) error {
	if useTor && autoApprove && os.Getenv("CONNER_I_UNDERSTAND_OPEN_RELAY") != "1" {
		return fmt.Errorf("refusing --auto-approve with Tor (open relay). set CONNER_I_UNDERSTAND_OPEN_RELAY=1 to override")
	}

	srv := server.NewServer()
	srv.AutoApprove = autoApprove

	bind := "127.0.0.1:" + port
	if lan {
		bind = "0.0.0.0:" + port
	}

	var et *tor.EmbeddedTor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := srv.StartOn(bind); err != nil {
			fmt.Fprintln(os.Stderr, "hub:", err)
			cancel()
		}
	}()

	if useTor {
		if pt == "system-tor" {
			fmt.Println("Using system Tor SOCKS 127.0.0.1:9050")
			fmt.Println("HiddenServicePort " + port + " 127.0.0.1:" + port)
		} else {
			fmt.Println("Starting embedded Tor (CGO). Bootstrap can take ~30–90s…")
			var err error
			et, err = tor.StartEmbedded(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "embedded Tor failed: %v\nfalling back to system Tor SOCKS 127.0.0.1:9050\n", err)
				fmt.Println("Publish HiddenServicePort " + port + " 127.0.0.1:" + port)
			} else {
				defer et.Stop()
				<-srv.Ready
				onion, err := et.CreateServerOnion(ctx, atoi(port), srv.HTTPPort)
				if err != nil {
					fmt.Fprintf(os.Stderr, "onion: %v (hub still on %s)\n", err, bind)
				} else {
					srv.Stats.TorAddress = onion
					blob := invite.Encode(invite.Blob{Onion: onion, Port: port, Tor: true, Room: config.DefaultRoom})
					fmt.Println("Onion:", onion)
					fmt.Println("Invite (no DNS):", blob)
				}
			}
		}
	} else {
		fmt.Println("LAN/direct hub on", bind)
		fmt.Println("Invite:", invite.Encode(invite.Blob{Host: "127.0.0.1", Port: port, Tor: false, Room: config.DefaultRoom}))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	if noTUI {
		<-sig
		srv.Stop()
		return nil
	}

	p := tea.NewProgram(servertui.InitialModel(srv), tea.WithAltScreen())
	go func() {
		<-sig
		p.Quit()
	}()
	_, err := p.Run()
	srv.Stop()
	return err
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 6666
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return 6666
	}
	return n
}

func usage() {
	fmt.Fprintf(os.Stderr, `CONNER v%s — onion-first group TUI (member E2EE)

Hub:
  conner --server --tor
  conner --server --lan --port 6666          # bind all interfaces (LAN)
  conner --server --tor --pt system-tor

Join (invite blob or host:port, no DNS required):
  conner <nick> <conner://v1/...>
  conner --tor <nick> <onion>:6666
  conner <nick> 127.0.0.1:6666

Flags: --auto-approve (forbidden with --tor unless CONNER_I_UNDERSTAND_OPEN_RELAY=1)
       --passphrase   wrap identity keys (also CONNER_PASSPHRASE)
	       --pt system-tor

Data dir: ~/.conner (override CONNER_HOME)
`, config.Version)
}
