package embed_util_test

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/auron-labs/go-embed-python/embed_util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testFile struct {
	data []byte
	mode os.FileMode
}

func binaryData() []byte {
	b := make([]byte, 100*1024)
	_, _ = rand.New(rand.NewSource(1)).Read(b)
	return b
}

func baseFiles() map[string]testFile {
	return map[string]testFile{
		"top.txt":           {[]byte("top level text\n"), 0o644},
		"sub/a.txt":         {[]byte("text in a sub dir\n"), 0o644},
		"sub/deep/b.bin":    {binaryData(), 0o644},
		"sub/deep/run.sh":   {[]byte("#!/bin/sh\necho hi\n"), 0o755},
		"sub/deep/empty.go": {nil, 0o644},
	}
}

func writeSource(t *testing.T, files map[string]testFile, withSymlink bool) string {
	dir := t.TempDir()
	for name, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, f.data, f.mode))
		require.NoError(t, os.Chmod(path, f.mode))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "emptydir"), 0o755))
	if withSymlink {
		require.NoError(t, os.Symlink(filepath.Join("..", "sub", "a.txt"), filepath.Join(dir, "emptydir", "link.txt")))
	}
	return dir
}

func embedDir(t *testing.T, files map[string]testFile, withSymlink bool) string {
	out := t.TempDir()
	require.NoError(t, embed_util.CopyForEmbed(out, writeSource(t, files, withSymlink)))
	return out
}

func extract(t *testing.T, embedded string, tmpDir string, withHash bool) *embed_util.EmbeddedFiles {
	e, err := embed_util.NewEmbeddedFilesWithTmpDir(os.DirFS(embedded), tmpDir, withHash)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Cleanup() })
	return e
}

func readFile(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestExtractContent(t *testing.T) {
	symlinks := runtime.GOOS != "windows"
	files := baseFiles()
	e := extract(t, embedDir(t, files, symlinks), filepath.Join(t.TempDir(), "x"), true)

	for name, f := range files {
		path := filepath.Join(e.GetExtractedPath(), filepath.FromSlash(name))
		st, err := os.Lstat(path)
		require.NoError(t, err, name)
		assert.True(t, st.Mode().IsRegular(), name)
		if runtime.GOOS != "windows" {
			assert.Equal(t, f.mode, st.Mode().Perm(), name)
		}
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(f.data, got), name)
	}

	st, err := os.Stat(filepath.Join(e.GetExtractedPath(), "emptydir"))
	require.NoError(t, err)
	assert.True(t, st.IsDir())

	if symlinks {
		// symlinks are materialized as copies of their target
		link := filepath.Join(e.GetExtractedPath(), "emptydir", "link.txt")
		st, err := os.Lstat(link)
		require.NoError(t, err)
		assert.True(t, st.Mode().IsRegular())
		assert.Equal(t, string(files["sub/a.txt"].data), readFile(t, link))
	}
}

func TestMatchingMarkerSkipsExtraction(t *testing.T) {
	embedded := embedDir(t, baseFiles(), false)
	tmpDir := filepath.Join(t.TempDir(), "x")

	e1 := extract(t, embedded, tmpDir, false)
	sentinel := filepath.Join(e1.GetExtractedPath(), "sentinel")
	require.NoError(t, os.WriteFile(sentinel, []byte("x"), 0o644))
	modified := filepath.Join(e1.GetExtractedPath(), "top.txt")
	require.NoError(t, os.WriteFile(modified, []byte("changed"), 0o644))

	extract(t, embedded, tmpDir, false)

	assert.FileExists(t, sentinel)
	assert.Equal(t, "changed", readFile(t, modified))
}

func TestNoHashModeReextractsChangedContent(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "x")

	v1 := baseFiles()
	v1["same-size.txt"] = testFile{[]byte("aaaa"), 0o644}
	v1["removed.txt"] = testFile{[]byte("gone soon"), 0o644}
	e1 := extract(t, embedDir(t, v1, false), tmpDir, false)
	require.NoError(t, e1.Cleanup())

	v2 := baseFiles()
	v2["same-size.txt"] = testFile{[]byte("bbbb"), 0o644}
	e2 := extract(t, embedDir(t, v2, false), tmpDir, false)

	assert.Equal(t, "bbbb", readFile(t, filepath.Join(e2.GetExtractedPath(), "same-size.txt")))
	assert.NoFileExists(t, filepath.Join(e2.GetExtractedPath(), "removed.txt"))
}

func TestCleanupKeepsFilesUsedByOthers(t *testing.T) {
	embedded := embedDir(t, baseFiles(), false)
	tmpDir := filepath.Join(t.TempDir(), "x")

	e1 := extract(t, embedded, tmpDir, true)
	e2 := extract(t, embedded, tmpDir, true)
	path := e1.GetExtractedPath()
	require.Equal(t, path, e2.GetExtractedPath())

	require.NoError(t, e1.Cleanup())
	assert.DirExists(t, path)

	require.NoError(t, e2.Cleanup())
	assert.NoDirExists(t, path)

	// idempotent
	require.NoError(t, e2.Cleanup())
	assert.Empty(t, e2.GetExtractedPath())
}

func TestStaleExtractionsAreRemoved(t *testing.T) {
	parent := t.TempDir()
	tmpDir := filepath.Join(parent, "x")

	staleDir := tmpDir + "-0123456789abcdef"
	require.NoError(t, os.MkdirAll(filepath.Join(staleDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staleDir, "sub", "f"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(staleDir+".lock", nil, 0o600))
	unrelated := tmpDir + "-not-a-hash"
	require.NoError(t, os.MkdirAll(unrelated, 0o755))

	// an extraction of another version that is still in use
	inUse := extract(t, embedDir(t, map[string]testFile{"old.txt": {[]byte("old"), 0o644}}, false), tmpDir, true)

	current := extract(t, embedDir(t, baseFiles(), false), tmpDir, true)

	assert.NoDirExists(t, staleDir)
	if runtime.GOOS != "windows" {
		assert.NoFileExists(t, staleDir+".lock")
	}
	assert.DirExists(t, unrelated)
	assert.DirExists(t, inUse.GetExtractedPath())
	assert.DirExists(t, current.GetExtractedPath())
}
