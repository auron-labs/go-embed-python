// Command generate bootstraps pip and packs the pip package for embedding.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/auron-labs/go-embed-python/pip"
	"github.com/auron-labs/go-embed-python/python"
)

func main() {
	targetDir := "./pip/internal/data"

	// ensure we have a stable extract path for the python distribution (otherwise shebangs won't be stable)
	tmpDir := filepath.Join("/tmp", "python-pip-bootstrap")
	ep, err := python.NewEmbeddedPythonWithTmpDir(tmpDir, false)
	if err != nil {
		panic(err)
	}
	defer func() { _ = ep.Cleanup() }()

	bootstrapPip(ep)

	err = pip.CreateEmbeddedPipPackages2(ep, "./pip/internal/requirements.txt", "", "", nil, targetDir)
	if err != nil {
		panic(err)
	}
}

func bootstrapPip(ep *python.EmbeddedPython) {
	getPip := downloadGetPip()
	defer func() { _ = os.Remove(getPip) }()

	cmd, err := ep.PythonCmd(getPip)
	if err != nil {
		panic(err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		panic(err)
	}
}

func downloadGetPip() string {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://bootstrap.pypa.io/get-pip.py", http.NoBody)
	if err != nil {
		panic(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		panic("failed to download get-pip.py: " + resp.Status)
	}

	tmpFile, err := os.CreateTemp("", "get-pip.py")
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		panic(err)
	}
	err = tmpFile.Close()
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		panic(fmt.Errorf("failed to write %s: %w", tmpFile.Name(), err))
	}

	return tmpFile.Name()
}
