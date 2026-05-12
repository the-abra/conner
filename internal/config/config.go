package config

import "time"

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
	MsgTypeDownloadReq = "DOWNLOAD_REQ"
	MsgTypeMediaRegister = "MEDIA_REGISTER"
	MsgTypeTyping = "TYPING"
	MsgTypeKeyShare = "KEY_SHARE"
	MsgTypeFileOffer = "FILE_OFFER"
)
