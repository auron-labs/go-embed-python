// Command generate downloads python-build-standalone distributions and packs them into the embedded data package.
package main

import (
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/auron-labs/go-embed-python/embed_util"
	"github.com/auron-labs/go-embed-python/internal"
	"github.com/gobwas/glob"
	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"
)

var (
	pythonStandaloneVersion = flag.String("python-standalone-version", "", "specify the python-standalone version. Check https://github.com/astral-sh/python-build-standalone/releases/ for available options.")
	pythonVersion           = flag.String("python-version", "", "specify the python version.")
	preparePath             = flag.String("prepare-path", filepath.Join(os.TempDir(), "python-download"), "specify the path where the python executables are downloaded and prepared. automatically creates a temporary directory if unset")
	runPrepare              = flag.Bool("prepare", true, "if set, python executables will be downloaded and prepared for packing at the configured path")
	runPack                 = flag.Bool("pack", true, "if set, previously prepared python executables will be packed into their redistributable form")
	pythonVersionBase       string
)

var archMapping = map[string]string{
	"amd64": "x86_64",
	"386":   "i686",
	"arm64": "aarch64",
}

var removeLibs = []string{
	"ensurepip",
	"idlelib",
	"lib2to3",
	"pydoc_data",
	"site-packages",
	"test",
	"turtledemo",
	"tkinter",
	"lib-dynload/_tkinter*",
	"lib-dynload/_test*",
	"lib-dynload/_ctypes_test*",
	"bin", // not really a library, but erroneously installed by jsonpath_ng
}

// removeInstallGlobs are globs relative to the install dir, applied before CleanupPythonDir.
// bin/pip* and bin/idle* are broken because pip and idlelib are removed. The python binaries
// do not load libpython, so the shared library (and the python-config script that refers to it) is dead weight.
var removeInstallGlobs = map[string][]string{
	"linux": {
		"bin/pip*", "bin/idle*", "bin/python3*-config",
		"lib/libpython3*.so*",
		"lib/libtcl*", "lib/itcl*", "lib/thread*", "lib/tcl*", "lib/tk*",
	},
	"darwin": {
		"bin/pip*", "bin/idle*", "bin/python3*-config",
		"lib/libpython3*.dylib",
		"lib/libtcl*", "lib/itcl*", "lib/thread*", "lib/tcl*", "lib/tk*",
	},
	"windows": {
		"tcl",
		"DLLs/tcl*.dll", "DLLs/tk*.dll",
		"DLLs/_tkinter.pyd", "DLLs/_test*.pyd", "DLLs/_ctypes_test.pyd",
	},
}

var keepNixPatterns = []glob.Glob{
	glob.MustCompile("bin/**"),
	glob.MustCompile("lib/*.so*"),
	glob.MustCompile("lib/*.dylib"),
	glob.MustCompile("lib/python3.*/**"),
}

var keepWinPatterns = []glob.Glob{
	glob.MustCompile("Lib/**"),
	glob.MustCompile("DLLs/**"),
	glob.MustCompile("*.dll"),
	glob.MustCompile("*.exe"),
}

var downloadLock sync.Mutex

func main() {
	flag.Parse()

	if *pythonVersion == "" || *pythonStandaloneVersion == "" {
		log.Fatal("missing flags")
	}

	log.Infof("python-standalone-version=%s", *pythonStandaloneVersion)
	log.Infof("python-version=%s", *pythonVersion)

	pythonVersionBase = strings.Join(strings.Split(*pythonVersion, ".")[0:2], ".")

	targetPath := "./python/internal/data"

	var wg sync.WaitGroup

	type job struct {
		os           string
		arch         string
		dist         string
		ext          string
		keepPatterns []glob.Glob
		strip        bool
	}

	jobs := []job{
		{"linux", "amd64", "unknown-linux-gnu-pgo+lto-full", "tar.zst", keepNixPatterns, true},
		{"linux", "arm64", "unknown-linux-gnu-pgo+lto-full", "tar.zst", keepNixPatterns, true},
		{"darwin", "amd64", "apple-darwin-pgo+lto-full", "tar.zst", keepNixPatterns, false},
		{"darwin", "arm64", "apple-darwin-pgo+lto-full", "tar.zst", keepNixPatterns, false},
		{"windows", "amd64", "pc-windows-msvc-pgo-full", "tar.zst", keepWinPatterns, false},
	}
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			if *runPrepare {
				downloadAndPrepare(j.os, j.arch, j.dist, j.ext, j.keepPatterns, j.strip)
			}
			if *runPack {
				packPrepared(j.os, j.arch, j.dist, j.ext, targetPath)
			}
			wg.Done()
		}()
	}
	wg.Wait()

	if *runPack {
		// the placeholder declares an empty Data, which would conflict with the generated embed files
		err := os.Remove(filepath.Join(targetPath, "dummy.go"))
		if err != nil && !os.IsNotExist(err) {
			log.Fatal(err)
		}
	}
}

func calcInstallPath(extractPath string, dist string) string {
	installPath := filepath.Join(extractPath, "python")
	if !strings.Contains(dist, "install_only") {
		installPath = filepath.Join(installPath, "install")
	}
	return installPath
}

func downloadAndPrepare(osName string, arch string, dist string, ext string, keepPatterns []glob.Glob, strip bool) {
	downloadPath, err := download(arch, dist, ext)
	if err != nil {
		log.Errorf("download failed: %v", err)
		os.Exit(1)
	}

	extractPath := downloadPath + ".extracted"
	err = os.RemoveAll(extractPath)
	if err != nil {
		log.Panic(err)
	}

	_, err = extract(downloadPath, extractPath, ext)
	if err != nil {
		log.Errorf("extract failed: %v", err)
		os.Exit(1)
	}

	installPath := calcInstallPath(extractPath, dist)

	var libPath string
	if osName == "windows" {
		libPath = filepath.Join(installPath, "Lib")
	} else {
		libPath = filepath.Join(installPath, "lib", "python"+pythonVersionBase)
	}

	for _, lib := range removeLibs {
		removeGlob(filepath.Join(libPath, lib))
	}
	for _, g := range removeInstallGlobs[osName] {
		removeGlob(filepath.Join(installPath, g))
	}

	err = internal.CleanupPythonDir(installPath, keepPatterns)
	if err != nil {
		panic(err)
	}

	if strip {
		err = internal.StripBinaries(installPath)
		if err != nil {
			panic(err)
		}
	}
}

func removeGlob(pattern string) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		log.Panic(err)
	}
	for _, m := range matches {
		_ = os.RemoveAll(m)
	}
}

func packPrepared(osName string, arch string, dist string, ext string, targetPath string) {
	extractPath := generateDownloadPath(arch, dist, ext) + ".extracted"
	installPath := calcInstallPath(extractPath, dist)
	err := embed_util.CopyForEmbed(filepath.Join(targetPath, fmt.Sprintf("%s-%s", osName, arch)), installPath)
	if err != nil {
		panic(err)
	}

	err = embed_util.WriteEmbedGoFile(targetPath, osName, arch)
	if err != nil {
		panic(err)
	}

	f, err := os.Create(filepath.Join(targetPath, "PYTHON_VERSION")) //nolint:gosec // path is derived from the generator target
	if err != nil {
		panic(err)
	}
	_, err = fmt.Fprintf(f, "PYTHON_VERSION=%q\nPYTHON_STANDALONE_VERSION=%q\n", *pythonVersion, *pythonStandaloneVersion)
	if err != nil {
		_ = f.Close()
		panic(err)
	}
	err = f.Close()
	if err != nil {
		panic(err)
	}
}

func generateDownloadPath(arch string, dist string, ext string) string {
	pythonArch, ok := archMapping[arch]
	if !ok {
		log.Errorf("arch %s not supported", arch)
		os.Exit(1)
	}
	fname := fmt.Sprintf("cpython-%s+%s-%s-%s.%s", *pythonVersion, *pythonStandaloneVersion, pythonArch, dist, ext)
	return filepath.Join(*preparePath, fname)
}

func download(arch string, dist string, ext string) (string, error) {
	downloadLock.Lock()
	defer downloadLock.Unlock()

	downloadPath := generateDownloadPath(arch, dist, ext)
	fname := filepath.Base(downloadPath)
	downloadURL := fmt.Sprintf("https://github.com/astral-sh/python-build-standalone/releases/download/%s/%s", *pythonStandaloneVersion, fname)

	if _, err := os.Stat(downloadPath); err == nil {
		log.Infof("skipping download of %s", downloadURL)
		return downloadPath, nil
	}

	err := os.MkdirAll(filepath.Dir(downloadPath), 0o755) //nolint:gosec // cache dir for downloads
	if err != nil {
		return "", fmt.Errorf("mkdirs failed: %w", err)
	}
	log.Infof("downloading %s", downloadURL)

	err = downloadFile(downloadURL, downloadPath)
	if err != nil {
		return "", err
	}

	return downloadPath, nil
}

// downloadFile streams url into a temp file next to dst and renames it on success,
// so a failed download never leaves a partial file that would be treated as cached.
func downloadFile(url string, dst string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", url, err)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download of %s failed: %w", url, err)
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode < 200 || r.StatusCode > 299 {
		return fmt.Errorf("download of %s failed: unexpected status: %s", url, r.Status)
	}

	f, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", dst, err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	_, err = io.Copy(f, r.Body)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write %s: %w", f.Name(), err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close %s: %w", f.Name(), err)
	}
	err = os.Chmod(f.Name(), 0o640) //nolint:gosec // downloaded archives are group-readable
	if err != nil {
		return fmt.Errorf("failed to chmod %s: %w", f.Name(), err)
	}
	err = os.Rename(f.Name(), dst)
	if err != nil {
		return fmt.Errorf("failed to move download to %s: %w", dst, err)
	}
	return nil
}

func extract(archivePath string, targetPath string, ext string) (string, error) {
	f, err := os.Open(archivePath) //nolint:gosec // archive path is derived from the generator flags
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", archivePath, err)
	}
	defer func() { _ = f.Close() }()

	var dc io.Reader
	switch ext {
	case "tar.gz":
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("failed to read gzip stream of %s: %w", archivePath, err)
		}
		defer func() { _ = gz.Close() }()
		dc = gz
	case "tar.zst":
		z, err := zstd.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("failed to read zstd stream of %s: %w", archivePath, err)
		}
		defer z.Close()
		dc = z
	default:
		return "", fmt.Errorf("unknown extension: %s", ext)
	}

	log.Infof("decompressing %s", archivePath)
	err = internal.ExtractTarStream(dc, targetPath)
	if err != nil {
		return "", fmt.Errorf("decompression failed: %w", err)
	}

	return targetPath, nil
}
