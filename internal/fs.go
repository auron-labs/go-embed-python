package internal

import (
	"os"
)

// Exists reports whether path can be stat'ed.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
