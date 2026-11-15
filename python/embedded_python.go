package python

import (
	"fmt"

	"github.com/kluctl/go-embed-python/embed_util"
	"github.com/kluctl/go-embed-python/python/internal/data"
)

// EmbeddedPython is a Python backed by an interpreter extracted from the embedded distribution.
type EmbeddedPython struct {
	e *embed_util.EmbeddedFiles
	Python
}

// NewEmbeddedPython creates a new EmbeddedPython instance. The embedded source code and python binaries are
// extracted on demand using the given name as the base for the temporary directory. You should ensure that the chosen
// name does collide with other consumers of this library.
func NewEmbeddedPython(name string) (*EmbeddedPython, error) {
	e, err := embed_util.NewEmbeddedFiles(data.Data, "python-"+name)
	if err != nil {
		return nil, fmt.Errorf("failed to extract python: %w", err)
	}
	return &EmbeddedPython{
		e:      e,
		Python: NewPython(WithPythonHome(e.GetExtractedPath())),
	}, nil
}

// NewEmbeddedPythonWithTmpDir is like NewEmbeddedPython but extracts to tmpDir, suffixed with the content hash
// if withHashInDir is set.
func NewEmbeddedPythonWithTmpDir(tmpDir string, withHashInDir bool) (*EmbeddedPython, error) {
	e, err := embed_util.NewEmbeddedFilesWithTmpDir(data.Data, tmpDir, withHashInDir)
	if err != nil {
		return nil, fmt.Errorf("failed to extract python to %s: %w", tmpDir, err)
	}
	return &EmbeddedPython{
		e:      e,
		Python: NewPython(WithPythonHome(e.GetExtractedPath())),
	}, nil
}

// Cleanup removes the extracted files unless other users still hold them.
func (ep *EmbeddedPython) Cleanup() error {
	err := ep.e.Cleanup()
	if err != nil {
		return fmt.Errorf("failed to clean up python: %w", err)
	}
	return nil
}

// GetExtractedPath returns the directory the python distribution was extracted to.
func (ep *EmbeddedPython) GetExtractedPath() string {
	return ep.e.GetExtractedPath()
}
