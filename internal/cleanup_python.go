// Package internal contains helpers for preparing and unpacking python distributions.
package internal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gobwas/glob"
)

// DefaultPythonRemovePatterns matches files and directories that CleanupPythonDir always removes.
var DefaultPythonRemovePatterns = []glob.Glob{
	glob.MustCompile("__pycache__"),
	glob.MustCompile("**/__pycache__"),
	glob.MustCompile("**.a"),
	glob.MustCompile("**.pdb"),
	glob.MustCompile("**.pyc"),
	glob.MustCompile("**/test_*.py"),
	glob.MustCompile("**/*.dist-info"),
}

// CleanupPythonDir removes caches, debug files, tests and metadata matching DefaultPythonRemovePatterns from dir.
// If keepPatterns is not empty, all files not matching any of the patterns are removed as well.
// Directories left empty are removed afterwards.
func CleanupPythonDir(dir string, keepPatterns []glob.Glob) error {
	var removes []string
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("failed to relativize %s: %w", path, err)
		}
		for _, p := range DefaultPythonRemovePatterns {
			if p.Match(relPath) {
				removes = append(removes, path)
			}
		}
		if len(keepPatterns) != 0 && !info.Mode().IsDir() {
			keep := false
			for _, p := range keepPatterns {
				if p.Match(relPath) {
					keep = true
					break
				}
			}
			if !keep {
				removes = append(removes, path)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk %s: %w", dir, err)
	}

	for _, r := range removes {
		err = os.RemoveAll(r)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", r, err)
		}
	}

	return removeEmptyDirs(dir)
}

func removeEmptyDirs(dir string) error {
	for {
		didRemove, err := removeEmptyDirs2(dir)
		if err != nil {
			return err
		}
		if !didRemove {
			break
		}
	}
	return nil
}

func removeEmptyDirs2(dir string) (bool, error) {
	var removes []string
	err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			des, err := os.ReadDir(path)
			if err != nil {
				return fmt.Errorf("failed to read dir %s: %w", path, err)
			}
			if len(des) == 0 {
				removes = append(removes, path)
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("failed to walk %s: %w", dir, err)
	}

	for _, r := range removes {
		err = os.Remove(r)
		if err != nil {
			return false, fmt.Errorf("failed to remove %s: %w", r, err)
		}
	}
	return len(removes) != 0, nil
}
