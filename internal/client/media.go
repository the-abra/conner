package client

import (
	"archive/tar"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxExtractSize is the decompression bomb limit (G110): 100 MB
const maxExtractSize = 100 * 1024 * 1024

// CreateMediaTarBase64 packs a single file into a tar archive and returns
// (basename, base64EncodedTar, error). The caller is responsible for supplying
// a trusted path obtained from the local user.
func CreateMediaTarBase64(path string) (string, string, error) {
	// #nosec G304 — user-supplied path is intentional (upload command)
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", "", fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", "", fmt.Errorf("stat %q: %w", path, err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("%q is a directory, only files are supported", path)
	}
	if info.Size() > maxExtractSize {
		return "", "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxExtractSize)
	}

	// Write tar to a temp file so we can base64-encode without buffering the
	// whole thing in memory before the header is finalized.
	tmpFile, err := os.CreateTemp("", "conner_media_*.tar")
	if err != nil {
		return "", "", fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)
	defer tmpFile.Close()

	tw := tar.NewWriter(tmpFile)
	hdr := &tar.Header{
		Name: filepath.Base(path),
		Mode: 0600,
		Size: info.Size(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return "", "", fmt.Errorf("tar header: %w", err)
	}
	if _, err := io.Copy(tw, file); err != nil {
		return "", "", fmt.Errorf("tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return "", "", fmt.Errorf("tar close: %w", err)
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return "", "", fmt.Errorf("seek: %w", err)
	}
	tarData, err := io.ReadAll(tmpFile)
	if err != nil {
		return "", "", fmt.Errorf("read tar: %w", err)
	}

	return filepath.Base(path), base64.StdEncoding.EncodeToString(tarData), nil
}

// ExtractMediaTarBase64 decodes a base64-encoded tar archive and extracts
// the first entry into destDir. Returns the absolute path of the saved file.
func ExtractMediaTarBase64(base64Data string, destDir string) (string, error) {
	tarData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(tarData) == 0 {
		return "", fmt.Errorf("empty tar data")
	}

	// G301: restrict directory permissions
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return "", fmt.Errorf("mkdir %q: %w", destDir, err)
	}

	// Work in memory — no need for a temp file on the receive side since
	// tarData is already decoded.
	tr := tar.NewReader(strings.NewReader(string(tarData)))
	hdr, err := tr.Next()
	if err != nil {
		return "", fmt.Errorf("tar read: %w", err)
	}

	// G305: prevent path traversal
	cleanName := filepath.Base(hdr.Name)
	if cleanName == "" || cleanName == "." || strings.Contains(cleanName, "..") {
		return "", fmt.Errorf("unsafe tar entry name: %q", hdr.Name)
	}
	destPath := filepath.Join(destDir, cleanName)

	// Double-check destPath stays within destDir after Join (symlink-safe)
	absBase, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("abs destDir: %w", err)
	}
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return "", fmt.Errorf("abs destPath: %w", err)
	}
	if !strings.HasPrefix(absDest, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal detected: %q escapes %q", destPath, destDir)
	}

	// G110: limit extracted bytes to prevent decompression bomb
	// #nosec G304 — path validated above
	outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create %q: %w", destPath, err)
	}
	defer outFile.Close()

	written, err := io.CopyN(outFile, tr, maxExtractSize)
	if err != nil && err != io.EOF {
		_ = os.Remove(destPath) // clean up partial file
		return "", fmt.Errorf("extract error after %d bytes (possible bomb): %w", written, err)
	}

	return absDest, nil
}
