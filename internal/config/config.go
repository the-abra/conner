package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// Network config
	ServerHost = "127.0.0.1"
	ServerPort = "6666"

	// Limits
	MaxClients         = 100
	MessageSizeLimit   = 8192
	MessageHistoryLimit = 100

	// Security
	RateLimitWindow   = 1.0 * time.Second
	RateLimitMessages = 5

	// Application limits
	MessageTTL = 24 * time.Hour
)

// Message types
const (
	MsgTypeJoin    = "JOIN"
	MsgTypeLeave   = "LEAVE"
	MsgTypeChat    = "CHAT"
	MsgTypePrivate = "PRIVATE"
	MsgTypeSystem  = "SYSTEM"
	MsgTypeError   = "ERROR"
	MsgTypePing    = "PING"
	MsgTypePong    = "PONG"
	MsgTypeUserList = "USER_LIST"
	MsgTypeMediaInfo = "MEDIA_INFO"
	MsgTypeMediaData = "MEDIA_DATA"
	MsgTypeTyping = "TYPING"
	MsgTypeKeyShare = "KEY_SHARE"
	MsgTypeFileOffer = "FILE_OFFER"
)

// Tor Config
var (
	TorrcPath       string
	TorCookiePath   string
	TorDataDir      string
	TorControlAddr  = "127.0.0.1:9051"
	TorSocksAddr    = "127.0.0.1:9050"
)

func init() {
	// Portability: Use a local directory for all Tor-related data to avoid root requirement
	wd, _ := os.Getwd()
	baseDir := filepath.Join(wd, ".conner_data")
	_ = os.MkdirAll(baseDir, 0700)

	TorDataDir = filepath.Join(baseDir, "tor")
	TorrcPath = filepath.Join(baseDir, "torrc")
	TorCookiePath = filepath.Join(TorDataDir, "control_auth_cookie")

	_ = os.MkdirAll(TorDataDir, 0700)
}

func GetTorrcTemplate(isServer bool) string {
	port := "8888" // Client P2P port
	dir := "conner_client"
	if isServer {
		port = "6666" // Server port
		dir = "conner_chat"
	}

	// Note: We removed "User tor" to allow running as a non-privileged user.
	// We also use local absolute paths.
	return fmt.Sprintf(`DataDirectory %s
Log err file /dev/null
SocksPort %s
ControlPort %s
CookieAuthentication 1
HiddenServiceDir %s/%s/
HiddenServicePort 80 127.0.0.1:%s
`, TorDataDir, TorSocksAddr, TorControlAddr, TorDataDir, dir, port)
}

