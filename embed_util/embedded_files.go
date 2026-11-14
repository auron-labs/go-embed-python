package embed_util

import (
	"fmt"
	"github.com/gofrs/flock"
	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var staleHashRegex = regexp.MustCompile("^[0-9a-f]{16}$")

const extractedMarker = ".extracted"

type EmbeddedFiles struct {
	tmpDir        string
	extractedPath string
	// shared lock held while the extracted files are in use
	lock *flock.Flock
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

// Cleanup removes the extracted files unless other users (processes or other EmbeddedFiles instances) still hold them.
func (e *EmbeddedFiles) Cleanup() error {
	if e.lock == nil {
		return nil
	}
	lock := e.lock
	extractedPath := e.extractedPath
	e.lock = nil
	e.extractedPath = ""

	err := lock.Unlock()
	if err != nil {
		return err
	}
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer lock.Close()

	return os.RemoveAll(extractedPath)
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

	// Fast path: already extracted, only need to register as a user. An exclusive lock
	// would needlessly wait for every other user of the same extraction.
	err = lock.RLock()
	if err != nil {
		return err
	}
	if e.isExtracted(flHash) {
		e.lock = lock
		return e.afterExtract(withHashInDir)
	}
	err = lock.Unlock()
	if err != nil {
		return err
	}

	err = lock.Lock()
	if err != nil {
		return err
	}

	err = e.extractLocked(embedFs, fl, flHash)
	if err != nil {
		_ = lock.Unlock()
		return err
	}

	// Keep holding the files for our lifetime. In no-hash mode, a process that needs to re-extract
	// different content blocks until all users are gone, as replacing files would break running interpreters.
	err = lock.Unlock()
	if err != nil {
		return err
	}
	err = lock.RLock()
	if err != nil {
		return err
	}
	e.lock = lock
	return e.afterExtract(withHashInDir)
}

func (e *EmbeddedFiles) afterExtract(withHashInDir bool) error {
	if withHashInDir {
		e.removeStaleExtractions()
	}
	return nil
}

func (e *EmbeddedFiles) isExtracted(flHash string) bool {
	marker, err := os.ReadFile(filepath.Join(e.extractedPath, extractedMarker))
	return err == nil && string(marker) == flHash
}

// removeStaleExtractions removes "<tmpDir>-<hash>" extractions (and their lock files) left over from other
// versions, unless another user still holds them. Failures are ignored as this is best-effort housekeeping.
func (e *EmbeddedFiles) removeStaleExtractions() {
	entries, err := os.ReadDir(filepath.Dir(e.tmpDir))
	if err != nil {
		log.Debugf("failed to list stale extractions: %v", err)
		return
	}

	prefix := filepath.Base(e.tmpDir) + "-"
	stale := map[string]struct{}{}
	for _, de := range entries {
		name := strings.TrimSuffix(de.Name(), ".lock")
		if !strings.HasPrefix(name, prefix) || !staleHashRegex.MatchString(name[len(prefix):]) {
			continue
		}
		path := filepath.Join(filepath.Dir(e.tmpDir), name)
		if path != e.extractedPath {
			stale[path] = struct{}{}
		}
	}

	for path := range stale {
		lock := flock.New(path + ".lock")
		locked, err := lock.TryLock()
		if err != nil || !locked {
			continue
		}
		err = os.RemoveAll(path)
		if err != nil {
			log.Debugf("failed to remove stale extraction %s: %v", path, err)
		} else {
			// removed while still locked so that no one can lock a file that is about to disappear
			_ = os.Remove(lock.Path())
		}
		_ = lock.Unlock()
	}
}

func (e *EmbeddedFiles) extractLocked(embedFs fs.FS, fl *fileList, flHash string) error {
	if e.isExtracted(flHash) {
		return nil
	}

	// No or stale marker: start from scratch so that changed, partially written
	// and no longer existing files can't survive from earlier extractions.
	err := os.RemoveAll(e.extractedPath)
	if err != nil {
		return err
	}
	err = os.MkdirAll(e.extractedPath, 0o755)
	if err != nil {
		return err
	}

	err = e.copyEmbeddedFilesToTmp(embedFs, fl)
	if err != nil {
		return err
	}

	// written last, so its presence means that the extraction completed
	return os.WriteFile(filepath.Join(e.extractedPath, extractedMarker), []byte(flHash), 0o644)
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
