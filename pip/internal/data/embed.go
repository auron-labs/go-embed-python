// Package data holds the embedded pip distribution.
package data

import "embed"

// Data holds the embedded pip distribution.
//
//go:embed all:*
var Data embed.FS
