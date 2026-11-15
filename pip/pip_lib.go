package pip

import (
	"fmt"

	"github.com/kluctl/go-embed-python/embed_util"
	"github.com/kluctl/go-embed-python/pip/internal/data"
)

// NewPipLib extracts the embedded pip distribution. The name distinguishes the extraction directory from other
// consumers; call Cleanup on the result when done.
func NewPipLib(name string) (*embed_util.EmbeddedFiles, error) {
	e, err := embed_util.NewEmbeddedFiles(data.Data, "pip-"+name)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pip: %w", err)
	}
	return e, nil
}
