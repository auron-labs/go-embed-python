package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

// StripBinaries strips all ELF executables below path using llvm-strip inside a debian docker container.
func StripBinaries(path string) error {
	var binFiles []string

	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(path, p)
		if err != nil {
			return fmt.Errorf("failed to relativize %s: %w", p, err)
		}
		if info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			b, err := os.ReadFile(p) //nolint:gosec // walking the distribution being built
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", p, err)
			}
			if len(b) >= 4 && b[0] == 0x7F && b[1] == 0x45 && b[2] == 0x4c && b[3] == 0x46 {
				binFiles = append(binFiles, relPath)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk %s: %w", path, err)
	}

	log.Infof("stripping %d files in %s", len(binFiles), path)

	script := `set -e
export DEBIAN_FRONTEND=noninteractive
apt update
apt install -y llvm
cd /host
`
	var scriptSb46 strings.Builder
	for _, file := range binFiles {
		fmt.Fprintf(&scriptSb46, "llvm-strip %s\n", file)
	}
	script += scriptSb46.String()

	//nolint:gosec // docker is run with the path of the distribution being built
	cmd := exec.CommandContext(context.Background(), "docker", "run", "-i", "-v", path+":/host", "-w", "/host", "debian:trixie", "sh", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to strip binaries in %s: %w", path, err)
	}
	return nil
}
