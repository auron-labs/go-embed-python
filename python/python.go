// Package python runs python interpreters, either from PATH or from an embedded distribution.
package python

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Python locates a python executable and builds commands for running it.
type Python interface {
	GetExeName() string
	GetExePath() (string, error)
	AddPythonPath(p string)
	PythonCmd(args ...string) (*exec.Cmd, error)
	PythonCmd2(args []string) (*exec.Cmd, error)
}

type python struct {
	pythonHome string
	pythonPath []string
}

// PythonOpt configures a Python created by NewPython.
type PythonOpt func(o *python)

// WithPythonHome makes the python use the distribution at home (PYTHONHOME) instead of the one found in PATH.
func WithPythonHome(home string) PythonOpt {
	return func(o *python) {
		o.pythonHome = home
	}
}

// NewPython creates a Python from the given options.
func NewPython(opts ...PythonOpt) Python {
	ep := &python{}

	for _, o := range opts {
		o(ep)
	}

	return ep
}

func (ep *python) GetExeName() string {
	if runtime.GOOS == "windows" {
		return "python.exe"
	}
	return "python3"
}

func (ep *python) GetExePath() (string, error) {
	if ep.pythonHome == "" {
		p, err := exec.LookPath(ep.GetExeName())
		if err != nil {
			return "", fmt.Errorf("failed to determine %s path: %w", ep.GetExeName(), err)
		}
		return p, nil
	}

	var p string
	if runtime.GOOS == "windows" {
		p = filepath.Join(ep.pythonHome, ep.GetExeName())
	} else {
		p = filepath.Join(ep.pythonHome, "bin", ep.GetExeName())
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("failed to determine %s path: %w", ep.GetExeName(), err)
	}
	return p, nil
}

func (ep *python) AddPythonPath(p string) {
	ep.pythonPath = append(ep.pythonPath, p)
}

func (ep *python) PythonCmd(args ...string) (*exec.Cmd, error) {
	return ep.PythonCmd2(args)
}

func (ep *python) PythonCmd2(args []string) (*exec.Cmd, error) {
	exePath, err := ep.GetExePath()
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(context.Background(), exePath, args...) //nolint:gosec // running the resolved python executable is the purpose of this library
	cmd.Env = os.Environ()

	if ep.pythonHome != "" {
		cmd.Env = append(cmd.Env, "PYTHONHOME="+ep.pythonHome)
	}

	if len(ep.pythonPath) != 0 {
		pythonPathEnv := "PYTHONPATH=" + strings.Join(ep.pythonPath, string(os.PathListSeparator))
		cmd.Env = append(cmd.Env, pythonPathEnv)
	}

	return cmd, nil
}
