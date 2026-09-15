package appdir

import (
	"os"
	"path/filepath"
)

// Root returns ~/.conner (or $CONNER_HOME if set).
func Root() string {
	if h := os.Getenv("CONNER_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		wd, _ := os.Getwd()
		return filepath.Join(wd, ".conner")
	}
	return filepath.Join(home, ".conner")
}

func Ensure() error {
	dirs := []string{
		Root(),
		Path("identity"),
		Path("tor"),
		Path("rooms"),
		Path("history"),
		Path("inbox"),
		Path("outbox"),
		Path("vault"),
		Path("queue"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func Path(elem ...string) string {
	parts := append([]string{Root()}, elem...)
	return filepath.Join(parts...)
}

func IdentityKey(nick string) string {
	return Path("identity", "identity_"+nick+".key")
}

func IdentityStore(nick string) string {
	return Path("identity", "identities_"+nick+".json")
}

func TorData() string {
	return Path("tor")
}

func RoomDir(roomID string) string {
	return Path("rooms", roomID)
}

func RoomOutbox(roomID string) string {
	return filepath.Join(RoomDir(roomID), "outbox")
}

func RoomInbox(roomID string) string {
	return filepath.Join(RoomDir(roomID), "inbox")
}

func HistoryFile(roomID string) string {
	return Path("history", roomID+".bin")
}

func WrappedKeyFile() string {
	return Path("identity", "master.wrap")
}

func IdentityKeyWrap(nick string) string {
	return Path("identity", "identity_"+nick+".key.wrap")
}

func QueueFile(nick string) string {
	return Path("queue", nick+".ndjson")
}
