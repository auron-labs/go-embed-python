// Package pip embeds pip and creates embedded pip package sets for use with the python package.
package pip

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/auron-labs/go-embed-python/embed_util"
	"github.com/auron-labs/go-embed-python/internal"
	"github.com/auron-labs/go-embed-python/python"
)

// CreateEmbeddedPipPackagesForKnownPlatforms installs the requirements for every supported OS and architecture
// and writes them as embeddable files below targetDir, one directory per platform.
func CreateEmbeddedPipPackagesForKnownPlatforms(requirementsFile string, targetDir string) error {
	platforms := map[string][]string{
		"darwin-amd64":  {"macosx_11_0_x86_64", "macosx_12_0_x86_64"},
		"darwin-arm64":  {"macosx_11_0_arm64", "macosx_12_0_arm64"},
		"linux-amd64":   {"manylinux_2_17_x86_64", "manylinux_2_28_x86_64", "manylinux2014_x86_64"},
		"linux-arm64":   {"manylinux_2_17_aarch64", "manylinux_2_28_aarch64", "manylinux2014_aarch64"},
		"windows-amd64": {"win_amd64"},
	}

	for goPlatform, pipPlatforms := range platforms {
		s := strings.Split(goPlatform, "-")
		goOs, goArch := s[0], s[1]
		err := CreateEmbeddedPipPackages(requirementsFile, goOs, goArch, pipPlatforms, targetDir)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateEmbeddedPipPackages installs the requirements for the given platform and writes them as embeddable files
// below targetDir. pipPlatforms are the pip platform tags to download wheels for; if empty, pip installs for the
// host platform. It bootstraps a temporary embedded python for running pip.
func CreateEmbeddedPipPackages(requirementsFile string, goOs string, goArch string, pipPlatforms []string, targetDir string) error {
	name := fmt.Sprintf("pip-%d", rand.Uint32()) //nolint:gosec // only a temp name suffix

	// ensure we have a stable extract path for the python distribution (otherwise shebangs won't be stable)
	tmpDir := filepath.Join("/tmp", fmt.Sprintf("python-pip-%s-%s", goOs, goArch))
	ep, err := python.NewEmbeddedPythonWithTmpDir(tmpDir, false)
	if err != nil {
		return fmt.Errorf("failed to extract python to %s: %w", tmpDir, err)
	}
	defer func() { _ = ep.Cleanup() }()

	pipLib, err := NewPipLib(name)
	if err != nil {
		return err
	}
	defer func() { _ = pipLib.Cleanup() }()

	ep.AddPythonPath(pipLib.GetExtractedPath())

	return CreateEmbeddedPipPackages2(ep, requirementsFile, goOs, goArch, pipPlatforms, targetDir)
}

// CreateEmbeddedPipPackages2 is like CreateEmbeddedPipPackages but uses the given python, which must already
// provide pip, instead of bootstrapping one.
func CreateEmbeddedPipPackages2(ep *python.EmbeddedPython, requirementsFile string, goOs string, goArch string, pipPlatforms []string, targetDir string) error {
	tmpDir, err := os.MkdirTemp("", "pip-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	err = pipInstall(ep, requirementsFile, pipPlatforms, tmpDir)
	if err != nil {
		return err
	}

	err = os.MkdirAll(targetDir, 0o755) //nolint:gosec // extracted files must stay world-readable
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", targetDir, err)
	}

	platformTargetDir := targetDir
	if goOs != "" {
		platformTargetDir = filepath.Join(platformTargetDir, fmt.Sprintf("%s-%s", goOs, goArch))
	}

	if internal.Exists(platformTargetDir) {
		err = os.RemoveAll(platformTargetDir)
		if err != nil {
			return fmt.Errorf("failed to remove %s: %w", platformTargetDir, err)
		}
	}

	err = os.Mkdir(platformTargetDir, 0o755) //nolint:gosec // extracted files must stay world-readable
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", platformTargetDir, err)
	}

	err = embed_util.CopyForEmbed(platformTargetDir, tmpDir)
	if err != nil {
		return fmt.Errorf("failed to pack pip packages into %s: %w", platformTargetDir, err)
	}

	err = embed_util.WriteEmbedGoFile(targetDir, goOs, goArch)
	if err != nil {
		return fmt.Errorf("failed to write embed file to %s: %w", targetDir, err)
	}

	return nil
}

func pipInstall(ep *python.EmbeddedPython, requirementsFile string, platforms []string, targetDir string) error {
	args := []string{"-m", "pip", "install", "-r", requirementsFile, "-t", targetDir}
	if len(platforms) != 0 {
		for _, p := range platforms {
			args = append(args, "--platform", p)
		}
		args = append(args, "--only-binary=:all:")
	}

	cmd, err := ep.PythonCmd(args...)
	if err != nil {
		return fmt.Errorf("failed to build pip command: %w", err)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("pip install failed: %w", err)
	}
	err = internal.CleanupPythonDir(targetDir, nil)
	if err != nil {
		return fmt.Errorf("failed to clean up %s: %w", targetDir, err)
	}
	return nil
}
