package python

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/auron-labs/go-embed-python/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedPython(t *testing.T) {
	rndName := fmt.Sprintf("test-%d", rand.Uint32())
	ep, err := NewEmbeddedPython(rndName)
	require.NoError(t, err)
	defer func() { _ = ep.Cleanup() }()
	path := ep.GetExtractedPath()
	assert.NotEmpty(t, path)
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

func TestPrintSystemInfo(t *testing.T) {
	getSystemInfo := `
import platform, sys

print("system info:")
print("sys.version=" + sys.version)

print("platform.python_version=" + platform.python_version())
print("platform.machine=" + platform.machine())
print("platform.version=" + platform.version())
print("platform.platform=" + platform.platform())
print("platform.release=" + platform.release())
print("platform.uname=" + str(platform.uname()))
print("platform.system=" + platform.system())
print("platform.processor=" + platform.processor())
`

	rndName := fmt.Sprintf("test-%d", rand.Uint32())
	ep, err := NewEmbeddedPython(rndName)
	require.NoError(t, err)
	defer func() { _ = ep.Cleanup() }()
	path := ep.GetExtractedPath()
	assert.NotEmpty(t, path)
	pexe, _ := ep.GetExePath()
	assert.True(t, internal.Exists(pexe))

	cmd, _ := ep.PythonCmd("-c", getSystemInfo)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	defer func() { _ = stdout.Close() }()
	defer func() { _ = stderr.Close() }()

	err = cmd.Start()
	require.NoError(t, err)

	stdoutStr, _ := io.ReadAll(stdout)
	stderrStr, _ := io.ReadAll(stderr)
	t.Log("stdout=" + string(stdoutStr))
	t.Log("stderr=" + string(stderrStr))

	err = cmd.Wait()
	assert.NoError(t, err)
}
