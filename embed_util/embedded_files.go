package embed_util

import (
	"fmt"
	"github.com/gofrs/flock"
	"github.com/klauspost/compress/zstd"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type EmbeddedFiles struct {
	tmpDir        string
	extractedPath string
}

func NewEmbeddedFiles(embedFs fs.FS, name string) (*EmbeddedFiles, error) {
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("go-embedded-%s", name))
	return NewEmbeddedFilesWithTmpDir(embedFs, tmpDir, true)
}

func NewEmbeddedFilesWithTmpDir(embedFs fs.FS, tmpDir string, withHashInDir bool) (*EmbeddedFiles, error) {
	e := &EmbeddedFiles{
		tmpDir: tmpDir,
	}
	err := e.extract(embedFs, withHashInDir)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (e *EmbeddedFiles) Cleanup() error {
	if e.extractedPath == "" {
		return nil
	}
	err := os.RemoveAll(e.extractedPath)
	e.extractedPath = ""
	return err
}

func (e *EmbeddedFiles) GetExtractedPath() string {
	return e.extractedPath
}

func (e *EmbeddedFiles) extract(embedFs fs.FS, withHashInDir bool) error {
	fl, err := e.readOrBuildFileList(embedFs)
	if err != nil {
		return err
	}

	flHash := fl.Hash()

	if withHashInDir {
		e.extractedPath = fmt.Sprintf("%s-%s", e.tmpDir, flHash[:16])
	} else {
		e.extractedPath = e.tmpDir
	}
	err = os.MkdirAll(filepath.Dir(e.extractedPath), 0o755)
	if err != nil {
		return err
	}

	lock := flock.New(e.extractedPath + ".lock")
	err = lock.Lock()
	if err != nil {
		return err
	}
	defer lock.Close()

	err = os.MkdirAll(e.extractedPath, 0o755)
	if err != nil {
		return err
	}

	err = e.copyEmbeddedFilesToTmp(embedFs, fl)
	if err != nil {
		return err
	}

	return nil
}

func (e *EmbeddedFiles) readOrBuildFileList(embedFs fs.FS) (*fileList, error) {
	flStr, err := fs.ReadFile(embedFs, "files.json")
	if err != nil {
		if os.IsNotExist(err) {
			return buildFileListFromFs(embedFs)
		}
		return nil, err
	}

	fl, err := readFileList(string(flStr))
	if err != nil {
		return nil, err
	}
	return fl, nil
}

func (e *EmbeddedFiles) copyEmbeddedFilesToTmp(embedFs fs.FS, fl *fileList) error {
	m := make(map[string]fileListEntry)

	for _, fle := range fl.Files {
		m[fle.Name] = fle
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return err
	}
	defer dec.Close()

	for _, fle := range fl.Files {
		resolvedFle := fle
		for resolvedFle.Mode.Type() == fs.ModeSymlink {
			if filepath.IsAbs(resolvedFle.Symlink) {
				return fmt.Errorf("abs path not allowed: %s", resolvedFle.Symlink)
			}
			sl := filepath.Clean(filepath.Join(filepath.Dir(resolvedFle.Name), resolvedFle.Symlink))
			fle2, ok := m[sl]
			if !ok {
				return fmt.Errorf("symlink %s at %s could not be resolved", resolvedFle.Symlink, resolvedFle.Name)
			}
			resolvedFle = fle2
			if resolvedFle.Mode.IsDir() {
				return fmt.Errorf("symlinked dirs not supported at the moment: %s -> %s", fle.Name, resolvedFle.Name)
			}
		}

		path := filepath.Join(e.extractedPath, fle.Name)
		existingSt, err := os.Lstat(path)
		if err == nil {
			if resolvedFle.Mode.Type() == existingSt.Mode().Type() {
				if resolvedFle.Mode.IsDir() {
					continue
				} else if existingSt.Size() == resolvedFle.Size {
					// unchanged
					continue
				}
			}
			err = os.RemoveAll(path)
			if err != nil {
				return err
			}
		}

		if fle.Mode.IsDir() {
			err := os.MkdirAll(path, resolvedFle.Mode.Perm())
			if err != nil {
				return err
			}
			continue
		} else if !resolvedFle.Mode.IsRegular() {
			continue
		}

		err = e.writeFile(embedFs, dec, resolvedFle, path)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *EmbeddedFiles) writeFile(embedFs fs.FS, dec *zstd.Decoder, fle fileListEntry, path string) error {
	name := fle.Name
	if fle.Compressed {
		name += ".zst"
	}
	src, err := embedFs.Open(name)
	if err != nil {
		return err
	}
	defer src.Close()

	var r io.Reader = src
	if fle.Compressed {
		err = dec.Reset(src)
		if err != nil {
			return err
		}
		r = dec
	}

	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fle.Mode.Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, r)
	if err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}
