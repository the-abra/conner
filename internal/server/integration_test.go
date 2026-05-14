package server_test

import (
	"conner/internal/client"
	"conner/internal/crypto"
	"conner/internal/server"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFullFlow(t *testing.T) {
	// 1. Setup
	os.RemoveAll("vault")
	os.RemoveAll("uploads")
	os.MkdirAll("uploads", 0755)

	srv := server.NewServer()
	go srv.Start("10001")
	time.Sleep(1 * time.Second) // Wait for server

	// 2. Connect Client
	cli, err := client.Connect("TestUser", "127.0.0.1:10001", false, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	
	// Start background sync for client
	cli.StartAutoSync()

	// 3. Test Chat
	// Whitelist the client first (simulate admin)
	all := srv.ClientManager.GetAllClients()
	if len(all) == 0 {
		t.Fatal("No clients found on server")
	}
	all[0].State = "WHITELISTED"

	// 4. Test File Sync
	testData := "INTEGRITY_CHECK_OK"
	err = os.WriteFile(filepath.Join("uploads", "test.txt"), []byte(testData), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for autonomous sync loop (5s polling)
	t.Log("Waiting for autonomous sync...")
	time.Sleep(12 * time.Second)

	// Verify vault
	files, _ := os.ReadDir("vault")
	if len(files) == 0 {
		t.Error("Vault is empty. Autonomous sync FAILED.")
	} else {
		t.Logf("Found %d files in vault", len(files))
		t.Logf("Sync SUCCESS: File %s found in vault", files[0].Name())
	}

	// 5. Test Memory Scrubbing (Wipe)
	key := []byte("VERY_SECRET_KEY_1234567890123456")
	crypto.Wipe(key)
	for _, b := range key {
		if b != 0 {
			t.Error("Memory wipe FAILED: key not zeroed")
		}
	}

	// Cleanup
	srv.Stop()
}
