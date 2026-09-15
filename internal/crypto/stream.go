package crypto

import (
	"encoding/binary"
	"io"
)

const streamChunk = ChunkSize

// EncryptReaderTo writes length-prefixed AEAD chunks (fileID bound in AAD).
func EncryptReaderTo(w io.Writer, r io.Reader, key []byte, fileID string) error {
	buf := make([]byte, streamChunk)
	var idx uint32
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			ct, e := EncryptChunk(key, fileID, idx, buf[:n])
			if e != nil {
				return e
			}
			var hdr [4]byte
			binary.BigEndian.PutUint32(hdr[:], uint32(len(ct)))
			if _, e := w.Write(hdr[:]); e != nil {
				return e
			}
			if _, e := w.Write(ct); e != nil {
				return e
			}
			idx++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			var end [4]byte
			_, e := w.Write(end[:])
			return e
		}
		if err != nil {
			return err
		}
	}
}

func DecryptReaderTo(w io.Writer, r io.Reader, key []byte, fileID string) error {
	var idx uint32
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n == 0 {
			return nil
		}
		if n > 2*streamChunk {
			return io.ErrUnexpectedEOF
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(r, ct); err != nil {
			return err
		}
		pt, err := DecryptChunk(key, fileID, idx, ct)
		if err != nil {
			return err
		}
		if _, err := w.Write(pt); err != nil {
			return err
		}
		idx++
	}
}
