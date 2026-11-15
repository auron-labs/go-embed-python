package embed_util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type fileList struct {
	ContentHash string          `json:"contentHash"`
	Files       []fileListEntry `json:"files"`
}

type fileListEntry struct {
	Name       string      `json:"name"`
	Size       int64       `json:"size"`
	Mode       fs.FileMode `json:"perm"`
	Symlink    string      `json:"symlink,omitempty"`
	Compressed bool        `json:"compressed,omitempty"`
}

func readFileList(fileListStr string) (*fileList, error) {
	var fl fileList
	err := json.Unmarshal([]byte(fileListStr), &fl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file list: %w", err)
	}
	return &fl, nil
}

func buildFileListFromDir(dir string) (*fileList, error) {
	var fl fileList

	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("failed to relativize %s: %w", path, err)
		}

		if relPath == "." || relPath == "files.json" {
			return nil
		}

		fle := fileListEntry{
			Name: relPath,
			Size: info.Size(),
			Mode: info.Mode(),
		}

		switch {
		case info.Mode().Type() == fs.ModeSymlink:
			sl, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}
			fle.Symlink = sl
			fle.Mode &= ^fs.ModePerm
		case info.Mode().IsDir():
			fle.Size = 0
		case info.Mode().IsRegular():
			fle.Compressed = true
		}

		fl.Files = append(fl.Files, fle)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk %s: %w", dir, err)
	}
	sort.Slice(fl.Files, func(i, j int) bool {
		return fl.Files[i].Name < fl.Files[j].Name
	})
	return &fl, nil
}

func buildFileListFromFs(embedFs fs.FS) (*fileList, error) {
	var fl fileList

	err := fs.WalkDir(embedFs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." || path == "files.json" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", path, err)
		}

		fle := fileListEntry{
			Name: path,
			Size: info.Size(),
			Mode: info.Mode() | 0o600,
		}

		switch {
		case info.Mode().Type() == fs.ModeSymlink:
			return errors.New("symlink not supported in buildFileListFromFs")
		case info.Mode().IsDir():
			fle.Size = 0
		}

		fl.Files = append(fl.Files, fle)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk embedded files: %w", err)
	}
	sort.Slice(fl.Files, func(i, j int) bool {
		return fl.Files[i].Name < fl.Files[j].Name
	})
	return &fl, nil
}

func (fl *fileList) Hash() string {
	h := sha256.New()
	e := json.NewEncoder(h)
	err := e.Encode(fl)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}
