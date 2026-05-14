package crypto

import (
	"runtime"
)

// Wipe overwrites the given byte slice with zeros to ensure sensitive data 
// (like keys) is removed from RAM. It uses runtime.KeepAlive to prevent 
// the compiler from optimizing away the zeroing operation if the slice 
// is not used afterwards.
func Wipe(data []byte) {
	if data == nil {
		return
	}
	for i := range data {
		data[i] = 0
	}
	// Ensure the slice is considered "live" until the zeroing is finished
	runtime.KeepAlive(data)
}
