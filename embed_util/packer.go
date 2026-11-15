package embed_util

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// CopyForEmbed copies dir to out in the layout read by EmbeddedFiles: regular files are zstd compressed and
// a files.json describing the tree is written. The tree is not modified, except for creating missing directories.
func CopyForEmbed(out string, dir string) error {
	fl, err := buildFileListFromDir(dir)
	if err != nil {
		return err
	}

	log.Infof("copying to %s with %d files", out, len(fl.Files))
	err = copyFiles(out, dir, fl)
	if err != nil {
		return err
	}

	return doWriteFilesList(dir, out, fl)
}

// BuildAndWriteFilesList writes files.json for dir in place, without copying or compressing any files.
func BuildAndWriteFilesList(dir string) error {
	fl, err := buildFileListFromDir(dir)
	if err != nil {
		return err
	}
	return doWriteFilesList(dir, dir, fl)
}

func doWriteFilesList(srcDir string, outDir string, fl *fileList) error {
	var err error
	fl.ContentHash, err = calcContentHash(srcDir, fl)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(fl, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode file list: %w", err)
	}

	listPath := filepath.Join(outDir, "files.json")
	err = os.WriteFile(listPath, b, 0o644) //nolint:gosec // generated source files are world-readable
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", listPath, err)
	}
	return nil
}

// WriteEmbedGoFile writes a Go file into targetDir that embeds the files for the given platform. If goOs is empty,
// everything in targetDir is embedded instead.
func WriteEmbedGoFile(targetDir string, goOs string, goArch string) error {
	var embedSrc, fname string
	if goOs == "" {
		embedSrc = `
package data

import "embed"

//go:embed all:*
var Data embed.FS
`
		fname = "embed.go"
	} else {
		embedSrc = fmt.Sprintf(`
package data

import (
	"embed"
	"io/fs"
)

//go:embed all:%s-%s
var _data embed.FS
var Data, _ = fs.Sub(_data, "%s-%s")
`, goOs, goArch, goOs, goArch)
		fname = strings.ReplaceAll(fmt.Sprintf("embed_%s_%s.go", goOs, goArch), "-", "_")
	}

	embedPath := filepath.Join(targetDir, fname)
	err := os.WriteFile(embedPath, []byte(embedSrc), 0o644) //nolint:gosec // generated source files are world-readable
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", embedPath, err)
	}
	return nil
}

func copyFiles(out string, dir string, fl *fileList) error {
	var g errgroup.Group
	g.SetLimit(8)

	// EncodeAll is safe for concurrent use
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	defer func() { _ = enc.Close() }()

	for _, fle := range fl.Files {
		path := filepath.Join(dir, fle.Name)

		st, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}

		outPath := filepath.Join(out, fle.Name)
		err = os.MkdirAll(filepath.Dir(outPath), 0o755) //nolint:gosec // generated source files are world-readable
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", filepath.Dir(outPath), err)
		}

		if !st.Mode().IsRegular() {
			continue
		}

		g.Go(func() error {
			data, err := os.ReadFile(path) //nolint:gosec // path is built from the file list of the directory being packed
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", path, err)
			}

			if fle.Compressed {
				data = enc.EncodeAll(data, make([]byte, 0, len(data)/2))
				outPath += ".zst"
			}

			err = os.WriteFile(outPath, data, 0o644) //nolint:gosec // generated source files are world-readable
			if err != nil {
				return fmt.Errorf("failed to write %s: %w", outPath, err)
			}
			return nil
		})
	}
	err = g.Wait()
	if err != nil {
		return fmt.Errorf("failed to copy files to %s: %w", out, err)
	}

	// ensure that the embedded files do not suffer from auto-conversion on windows
	attrPath := filepath.Join(out, ".gitattributes")
	err = os.WriteFile(attrPath, []byte("** binary\n"), 0o644) //nolint:gosec // generated source files are world-readable
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", attrPath, err)
	}

	return nil
}

// writeHashField adds a length-prefixed field to the hash, so that concatenated fields can't collide.
func writeHashField(hash io.Writer, data []byte) {
	_ = binary.Write(hash, binary.LittleEndian, uint64(len(data)))
	_, _ = hash.Write(data)
}

func calcContentHash(dir string, fl *fileList) (string, error) {
	hash := sha256.New()
	for _, fle := range fl.Files {
		path := filepath.Join(dir, fle.Name)
		st, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("failed to stat %s: %w", path, err)
		}
		switch {
		case st.Mode().Type() == os.ModeSymlink:
			sl, err := os.Readlink(path)
			if err != nil {
				return "", fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			writeHashField(hash, []byte("symlink"))
			writeHashField(hash, []byte(sl))
		case st.Mode().IsDir():
			err = os.MkdirAll(path, fle.Mode.Perm())
			if err != nil {
				return "", fmt.Errorf("failed to create %s: %w", path, err)
			}
			writeHashField(hash, []byte("dir"))
			writeHashField(hash, []byte(fle.Name))
		case st.Mode().IsRegular():
			data, err := os.ReadFile(path) //nolint:gosec // path is built from the file list of the directory being packed
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", path, err)
			}

			writeHashField(hash, []byte("regular"))
			writeHashField(hash, []byte(fle.Name))
			writeHashField(hash, data)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
