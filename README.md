# go-embed-python

[![CI](https://img.shields.io/github/actions/workflow/status/auron-labs/go-embed-python/ci.yml?branch=main&style=flat-square&label=ci)](https://github.com/auron-labs/go-embed-python/actions/workflows/ci.yml)
[![Go Reference](https://img.shields.io/badge/go-reference-007d9c?style=flat-square&logo=go)](https://pkg.go.dev/github.com/auron-labs/go-embed-python)
[![License](https://img.shields.io/github/license/auron-labs/go-embed-python?style=flat-square)](LICENSE)

Embed a complete Python interpreter in your Go binary and run it without CGO and without Python installed on the target host.

The library ships the [python-build-standalone](https://github.com/astral-sh/python-build-standalone) distributions via `//go:embed`, extracts the one for the current platform to a temporary directory on first use, and runs Python as a child process.

## Upstream

This repository is a fork of [kluctl/go-embed-python](https://github.com/kluctl/go-embed-python), created by Alexander Block and its contributors, and licensed under the [Apache License 2.0](LICENSE). The original work remains under that license and copyright. Changes made in this fork are recorded in its Git history and are also distributed under the Apache License 2.0.

## Features

- No CGO and no dynamic linking against Python: the interpreter runs in a separate process.
- Supported platforms: `linux-amd64`, `linux-arm64`, `darwin-amd64`, `darwin-arm64`, `windows-amd64`.
- Extraction happens once per version and is verified with a completion marker; later starts reuse it.
- Processes sharing an extraction don't remove it from under each other, and extractions of older versions are cleaned up automatically.
- Helpers to embed pip packages for every supported platform alongside the interpreter.

## Installation

Each release tag contains the embedded Python distribution. Pick the tag for the Python version you want from the [tags list](https://github.com/auron-labs/go-embed-python/tags):

```sh
go get github.com/auron-labs/go-embed-python@v0.0.0-3.14.6-20260610-1
```

The `main` branch contains no embedded distribution and cannot run Python; always depend on a release tag.

## Quick start

```go
package main

import (
	"os"

	"github.com/auron-labs/go-embed-python/python"
)

func main() {
	ep, err := python.NewEmbeddedPython("example")
	if err != nil {
		panic(err)
	}

	cmd, err := ep.PythonCmd("-c", "print('hello')")
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
```

`NewEmbeddedPython(name)` extracts the distribution to `<user cache dir>/go-embed-python/python-<name>-<hash>`, for example `~/.cache/go-embed-python/...` on Linux and `~/Library/Caches/go-embed-python/...` on macOS. Without a user cache dir, it uses a per-user directory in the OS temp dir. The directory must be owned by the current user and is refused otherwise. Choose a `name` that won't collide with other applications using this library. `PythonCmd` returns an `*exec.Cmd` with `PYTHONHOME` (and `PYTHONPATH`, if set via `AddPythonPath`) configured.

To control the extraction directory, use `python.NewEmbeddedPythonWithTmpDir(dir, withHashInDir)`. Choose a directory other users cannot write to: extraction paths are predictable, so a shared directory such as `/tmp` lets another user supply the files that get executed. With `withHashInDir` set to `false`, the same directory is reused across versions. A process that needs a different version waits until every process using the current extraction has exited.

`Cleanup()` removes the extracted files unless another process or instance is still using them.

## Versioning

Release tags have the form `v0.0.0-<python version>-<python-build-standalone release>-<build number>`. For example, `v0.0.0-3.14.6-20260610-1` is Python 3.14.6 from the [20260610](https://github.com/astral-sh/python-build-standalone/releases/tag/20260610) python-build-standalone release, build 1.

Because every tag is `v0.0.0`, dependency update tools such as Dependabot may propose unexpected Python version changes. Review them before merging.

## Embedding pip packages

Create a generator in your module, for example `internal/my-python-libs/generate/main.go`:

```go
package main

import (
	"github.com/auron-labs/go-embed-python/pip"
)

func main() {
	err := pip.CreateEmbeddedPipPackagesForKnownPlatforms("requirements.txt", "./data/")
	if err != nil {
		panic(err)
	}
}
```

Reference it from a Go file in the parent directory, e.g. `internal/my-python-libs/dummy.go`:

```go
package internal

//go:generate go run ./generate
```

Add `internal/my-python-libs/requirements.txt`:

```text
jinja2==3.1.2
```

`go generate ./...` installs the packages for every supported platform into `internal/my-python-libs/data`. Pass the generated `data.Data` to `embed_util.NewEmbeddedFiles()` to extract it, then add `EmbeddedFiles.GetExtractedPath()` to the interpreter with `ep.AddPythonPath(...)`.

[kluctl/go-jinja2](https://github.com/kluctl/go-jinja2) is a complete example of this pattern.

## Development

Tool versions and tasks are managed with [mise](https://mise.jdx.dev/):

```sh
mise run setup      # download Go modules and install Git hooks
mise run check      # run all linters and format checks
mise run fix        # apply automatic fixes
```

The `python` and `pip` packages need the embedded data, which only exists on release tags. To generate it locally (requires Docker for stripping Linux binaries):

```sh
mise run generate 3.14.6 20260610
mise run test
```

Generation deletes the placeholder `python/internal/data/dummy.go`; restore it with `git checkout python/internal/data` before committing.

The Python versions that get released are listed in [`.github/workflows/release.yml`](.github/workflows/release.yml). Pushing to `main` builds, tests and tags a release for every entry.

## License

[Apache License 2.0](LICENSE).
