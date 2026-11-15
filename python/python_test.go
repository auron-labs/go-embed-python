package python

import (
	"bytes"
	"io"
	"testing"

	"github.com/auron-labs/go-embed-python/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExternalPython(t *testing.T) {
	ep := NewPython()

	pexe, _ := ep.GetExePath()
	assert.True(t, internal.Exists(pexe))

	cmd, _ := ep.PythonCmd("-c", "print('test test')")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	defer func() { _ = stdout.Close() }()

	err = cmd.Start()
	require.NoError(t, err)

	stdoutStr, err := io.ReadAll(stdout)
	require.NoError(t, err)

	err = cmd.Wait()
	require.NoError(t, err)

	stdoutStr = bytes.TrimSpace(stdoutStr)
	assert.Equal(t, "test test", string(stdoutStr))
}
