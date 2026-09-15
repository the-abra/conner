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
	MaxClients          = 100
	MessageSizeLimit    = 8192
	MessageHistoryLimit = 1000

	// Security
	RateLimitWindow   = 1.0 * time.Second
	RateLimitMessages = 20

	// Application limits
	MessageTTL = 24 * time.Hour

	// Engine Version
	Version = "v3.0-ops"
	// ProtocolMajor is the incompatible handshake generation.
	ProtocolMajor = "v3"

	DefaultRoom = "general"
)

// Message types
const (
	MsgTypeJoin       = "JOIN"
	MsgTypeLeave      = "LEAVE"
	MsgTypeChat       = "CHAT"
	MsgTypePrivate    = "PRIVATE"
	MsgTypeSystem     = "SYSTEM"
	MsgTypeError      = "ERROR"
	MsgTypePing       = "PING"
	MsgTypePong       = "PONG"
	MsgTypeUserList   = "USER_LIST"
	MsgTypeTyping     = "TYPING"
	MsgTypeKeyShare   = "KEY_SHARE"
	MsgTypeRoomKey    = "ROOM_KEY"
	MsgTypeAck        = "ACK"
	MsgTypeReaction   = "REACTION"
	MsgTypeFileOffer  = "FILE_OFFER"
	MsgTypeFileChunk  = "FILE_CHUNK"
	MsgTypeRoomCreate = "ROOM_CREATE"
	MsgTypeRoomJoin   = "ROOM_JOIN"
	MsgTypeEpoch      = "EPOCH"
	MsgTypeSenderKey  = "SENDER_KEY"
	MsgTypeInvite     = "INVITE"
	MsgTypeCmd        = "CMD"
	MsgTypeFilePut    = "FILE_PUT"
	MsgTypeFileGet    = "FILE_GET"
	MsgTypeFileData   = "FILE_DATA"
)

// Tor Config
var (
	TorrcPath      string
	TorCookiePath  string
	TorDataDir     string
	TorControlAddr = "127.0.0.1:9051"
	TorSocksAddr   = "127.0.0.1:9050"
)

func init() {
	home, err := os.UserHomeDir()
	baseDir := filepath.Join(".conner_data")
	if env := os.Getenv("CONNER_HOME"); env != "" {
		baseDir = env
	} else if err == nil {
		baseDir = filepath.Join(home, ".conner")
	}
	_ = os.MkdirAll(baseDir, 0700)

	TorDataDir = filepath.Join(baseDir, "tor")
	TorrcPath = filepath.Join(baseDir, "torrc")
	TorCookiePath = filepath.Join(TorDataDir, "control_auth_cookie")

	_ = os.MkdirAll(TorDataDir, 0700)
}

func GetTorrcTemplate(isServer bool, chatPort, httpPort string) string {
	dir := "conner_client"
	if isServer {
		dir = "conner_chat"
	}

	template := fmt.Sprintf(`DataDirectory %s
Log err file /dev/null
SocksPort %s
ControlPort %s
CookieAuthentication 1
HiddenServiceDir %s/tor/%s/
`, TorDataDir, TorSocksAddr, TorControlAddr, TorDataDir, dir)

	if isServer {
		template += fmt.Sprintf("HiddenServicePort %s 127.0.0.1:%s\n", chatPort, chatPort)
		template += fmt.Sprintf("HiddenServicePort 80 127.0.0.1:%s\n", httpPort)
	} else {
		template += "HiddenServicePort 80 127.0.0.1:8888\n"
	}

	return template
}
