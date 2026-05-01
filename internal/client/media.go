package client

import (
	"archive/tar"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxExtractSize is the decompression bomb limit (G110): 100 MB
const maxExtractSize = 100 * 1024 * 1024

// CreateMediaTarBase64 packs a file or directory into a tar archive and returns
// (basename, base64EncodedTar, error).
func CreateMediaTarBase64(path string) (string, string, error) {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", "", fmt.Errorf("abs %q: %w", path, err)
	}
	_, err = os.Stat(absPath)
	if err != nil {
		return "", "", fmt.Errorf("stat %q: %w", path, err)
	}

	// Write tar to a temp file
	tmpFile, err := os.CreateTemp("", "conner_media_*.tar")
	if err != nil {
		return "", "", fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)
	defer tmpFile.Close()

	gw := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gw)
	baseDir := filepath.Dir(absPath)

	walkErr := filepath.Walk(absPath, func(p string, i os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if i.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(baseDir, p)
		if err != nil {
			return err
		}

		// #nosec G304
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()

		hdr, err := tar.FileInfoHeader(i, "")
		if err != nil {
			return err
		}
		hdr.Name = rel

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}
		return nil
	})

	if walkErr != nil {
		return "", "", fmt.Errorf("walk %q: %w", absPath, walkErr)
	}

	if err := tw.Close(); err != nil {
		return "", "", fmt.Errorf("tar close: %w", err)
	}
	if err := gw.Close(); err != nil {
		return "", "", fmt.Errorf("gzip close: %w", err)
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
// ALL entries into destDir. Returns the absolute path of the destination directory.
func ExtractMediaTarBase64(base64Data string, destDir string) (string, error) {
	tarData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(tarData) == 0 {
		return "", fmt.Errorf("empty tar data")
	}

	absBase, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("abs destDir: %w", err)
	}

	// #nosec G301
	if err := os.MkdirAll(absBase, 0777); err != nil {
		return "", fmt.Errorf("mkdir %q: %w", absBase, err)
	}

	gr, err := gzip.NewReader(strings.NewReader(string(tarData)))
	if err != nil {
		return "", fmt.Errorf("gzip read: %w", err)
	}
	defer gr.Close()

	var firstPath string
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("tar read: %w", err)
		}

		// G305: prevent path traversal
		destPath := filepath.Join(absBase, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(destPath), absBase) {
			return "", fmt.Errorf("path traversal detected: %q escapes %q", hdr.Name, absBase)
		}

		if firstPath == "" {
			firstPath = destPath
		}

		if hdr.Typeflag == tar.TypeDir {
			// #nosec G301
			if err := os.MkdirAll(destPath, 0777); err != nil {
				return "", fmt.Errorf("mkdir %q: %w", destPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(destPath), 0777); err != nil { // #nosec G301
			return "", fmt.Errorf("mkdir parent %q: %w", destPath, err)
		}

		// #nosec G304,G302
		outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			return "", fmt.Errorf("create %q: %w", destPath, err)
		}

		written, err := io.CopyN(outFile, tr, maxExtractSize)
		outFile.Close()
		if err != nil && err != io.EOF {
			_ = os.Remove(destPath)
			return "", fmt.Errorf("extract error after %d bytes: %w", written, err)
		}
	}

	return firstPath, nil
}
