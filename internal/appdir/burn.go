package appdir

import (
	"os"
	"path/filepath"
)

// LeftoverDirs are cwd names from older CONNER builds (best-effort wipe).
var LeftoverDirs = []string{"vault", ".conner_data", "uploads", "downloads"}

// Burn removes the profile tree (~/.conner or CONNER_HOME) and leftover cwd dirs.
// Best-effort: not a classified wipe, no multi-pass overwrite.
func Burn() error {
	root := Root()
	err := os.RemoveAll(root)
	wd, _ := os.Getwd()
	for _, d := range LeftoverDirs {
		_ = os.RemoveAll(filepath.Join(wd, d))
		_ = os.RemoveAll(d)
	}
	return err
}
