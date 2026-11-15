package internal

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractTarStream extracts the directories, regular files and symlinks of a tar stream into targetPath,
// overwriting existing files. Entries with names that could escape targetPath are rejected.
func ExtractTarStream(r io.Reader, targetPath string) error {
	tarReader := tar.NewReader(r)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("ExtractTarStream: Next() failed: %w", err)
		}

		if !validRelPath(header.Name) {
			return fmt.Errorf("tar contained invalid name error %q", header.Name)
		}

		p := filepath.FromSlash(header.Name)
		p = filepath.Join(targetPath, p)

		err = os.MkdirAll(filepath.Dir(p), 0o755) //nolint:gosec // extracted files must stay world-readable
		if err != nil {
			return fmt.Errorf("ExtractTarStream: MkdirAll() failed: %w", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(p, 0o755) //nolint:gosec // extracted files must stay world-readable
			if err != nil {
				return fmt.Errorf("ExtractTarStream: Mkdir() failed: %w", err)
			}
		case tar.TypeReg:
			_ = os.Remove(p)             // we allow overwriting, which easily happens on case insensitive filesystems
			outFile, err := os.Create(p) //nolint:gosec // names are validated by validRelPath
			if err != nil {
				return fmt.Errorf("ExtractTarStream: Create() failed: %w", err)
			}
			_, err = io.Copy(outFile, tarReader) //nolint:gosec // archive is a trusted python-build-standalone release
			_ = outFile.Close()
			if err != nil {
				return fmt.Errorf("ExtractTarStream: Copy() failed: %w", err)
			}
			err = os.Chmod(p, header.FileInfo().Mode())
			if err != nil {
				return fmt.Errorf("ExtractTarStream: Chmod() failed: %w", err)
			}
			err = os.Chtimes(p, header.AccessTime, header.ModTime)
			if err != nil {
				return fmt.Errorf("ExtractTarStream: Chtimes() failed: %w", err)
			}
		case tar.TypeSymlink:
			_ = os.Remove(p) // we allow overwriting, which easily happens on case insensitive filesystems
			if err := os.Symlink(header.Linkname, p); err != nil {
				return fmt.Errorf("ExtractTarStream: Symlink() failed: %w", err)
			}
		default:
			return fmt.Errorf("ExtractTarStream: uknown type %v in %v", header.Typeflag, header.Name)
		}
	}
	return nil
}

func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}
