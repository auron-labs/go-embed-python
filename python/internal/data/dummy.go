// Package data holds the embedded python distribution.
package data

import "embed"

// PLEASE READ THIS!!!!
// This file is really just a dummy. The release process will remove this file and generate some real embedded files
// and commit these into a temporary branch and then tag it. This is to avoid clogging up the main branch with too many
// binary files, which would be a very bad experience when pulling in go-embed-python as a dependency.

// Data is declared empty here so that the module builds and lints without the generated files.
var Data embed.FS
