package server_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"conner/internal/client"
	"conner/internal/config"
	"conner/internal/crypto"
	"conner/internal/invite"
	"conner/internal/protocol"
	"conner/internal/server"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func TestInviteRoundTrip(t *testing.T) {
	b := invite.Blob{Onion: "abc.onion", Port: "6666", Tor: true, Room: "ops"}
	s := invite.Encode(b)
	got, err := invite.Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Onion != b.Onion || got.Room != "ops" {
		t.Fatalf("invite mismatch: %+v", got)
	}
}

func TestHandshakeChatAndDM(t *testing.T) {
	t.Setenv("CONNER_HOME", t.TempDir())
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	srv := server.NewServer()
	srv.AutoApprove = true
	go func() { _ = srv.StartOn(addr) }()
	select {
	case <-srv.Ready:
	case <-time.After(8 * time.Second):
		t.Fatal("server not ready")
	}
	t.Cleanup(func() { srv.Stop() })

	alice, err := client.Connect("alice", addr, false, nil)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	defer alice.Cancel()
	bob, err := client.Connect("bob", addr, false, nil)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	defer bob.Cancel()

	// Drain until both have peer keys (user list).
	waitKeys := func(c *client.Client) {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			c.UserKeys["probe"] = nil // touch
			select {
			case msg := <-c.UpdateChan:
				if msg != nil && msg.Type == config.MsgTypeUserList {
					return
				}
			case <-time.After(200 * time.Millisecond):
			}
			if len(c.PeerE2E) > 0 {
				return
			}
		}
	}
	waitKeys(alice)
	waitKeys(bob)

	chat := protocol.CreateMessage(config.MsgTypeChat, "hello-from-alice", "alice")
	select {
	case alice.SendChan <- chat:
	default:
		t.Fatal("alice send blocked")
	}

	deadline := time.After(8 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("bob never saw chat")
		case msg := <-bob.UpdateChan:
			if msg == nil {
				continue
			}
			if msg.Type == config.MsgTypeChat && msg.Sender == "alice" {
				if msg.Content == "hello-from-alice" {
					return
				}
				// Retry: wait for flushed pending decrypt after SENDER_KEY.
				if msg.Content == "[waiting for sender key]" {
					continue
				}
			}
		}
	}
}

func TestVaultChunkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(src, []byte("vault-payload-xyz"), 0o600); err != nil {
		t.Fatal(err)
	}
	token := "tok"
	key := crypto.FileContentKey(token, "a.bin")
	encPath := filepath.Join(dir, "a.enc")
	in, _ := os.Open(src)
	out, _ := os.Create(encPath)
	if err := crypto.EncryptReaderTo(out, in, key, "a.bin"); err != nil {
		t.Fatal(err)
	}
	in.Close()
	out.Close()

	plain := filepath.Join(dir, "out.bin")
	ein, _ := os.Open(encPath)
	eout, _ := os.Create(plain)
	if err := crypto.DecryptReaderTo(eout, ein, key, "a.bin"); err != nil {
		t.Fatal(err)
	}
	ein.Close()
	eout.Close()
	got, _ := os.ReadFile(plain)
	if string(got) != "vault-payload-xyz" {
		t.Fatalf("%q", got)
	}
}

func TestClientAdminApprove(t *testing.T) {
	t.Setenv("CONNER_HOME", t.TempDir())
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := server.NewServer()
	srv.AutoApprove = true
	go func() { _ = srv.StartOn(addr) }()
	select {
	case <-srv.Ready:
	case <-time.After(8 * time.Second):
		t.Fatal("ready")
	}
	t.Cleanup(func() { srv.Stop() })
	alice, err := client.Connect("alice", addr, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Cancel()
	time.Sleep(200 * time.Millisecond)
	ac := srv.ClientManager.GetClientByNickname("alice")
	if ac == nil || !ac.IsAdmin {
		t.Fatal("alice should be first-member admin")
	}
	srv.AutoApprove = false
	bob, err := client.Connect("bob", addr, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Cancel()
	time.Sleep(150 * time.Millisecond)
	bc := srv.ClientManager.GetClientByNickname("bob")
	if bc == nil || bc.State != "PENDING" {
		t.Fatalf("bob pending %+v", bc)
	}
	alice.SendChan <- protocol.CreateMessage(config.MsgTypeCmd, "/approve bob", "alice")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bc = srv.ClientManager.GetClientByNickname("bob")
		if bc != nil && bc.State == "WHITELISTED" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("approve via CMD failed")
}

func TestMuxVaultFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CONNER_HOME", home)
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := server.NewServer()
	srv.AutoApprove = true
	go func() { _ = srv.StartOn(addr) }()
	select {
	case <-srv.Ready:
	case <-time.After(8 * time.Second):
		t.Fatal("ready")
	}
	t.Cleanup(func() { srv.Stop() })
	alice, err := client.Connect("alice", addr, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Cancel()
	bob, err := client.Connect("bob", addr, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Cancel()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if alice.VaultToken != "" {
			break
		}
		select {
		case <-alice.UpdateChan:
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	if alice.VaultToken == "" {
		t.Fatal("no vault token")
	}
	src := filepath.Join(home, "note.txt")
	if err := os.WriteFile(src, []byte("mux-hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := alice.UploadMux(src); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	dest := filepath.Join(home, "inbox-bob")
	if err := bob.DownloadMux("note.txt", dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mux-hello" {
		t.Fatalf("%q", got)
	}
}

func TestWipe(t *testing.T) {
	key := []byte("VERY_SECRET_KEY_1234567890123456")
	crypto.Wipe(key)
	for _, b := range key {
		if b != 0 {
			t.Fatal("wipe failed")
		}
	}
}
