package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain ensures tests find ffmpeg-full when it is installed. Homebrew's
// keg-only ffmpeg-full is required by the subtitle render path (libass) but
// is not symlinked into /opt/homebrew/bin, so a plain `go test ./internal/media`
// against the system PATH picks up the bare ffmpeg and the subtitles test
// fails. Prepending ffmpeg-full/bin (when present and missing from PATH) makes
// every existing test that calls exec.LookPath("ffmpeg") pick up the richer
// build without changing production code or test expectations.
func TestMain(m *testing.M) {
	if candidate, ok := lookupFFmpegFull(); ok {
		prependPath(candidate)
	}
	os.Exit(m.Run())
}

// lookupFFmpegFull finds the ffmpeg-full bin directory. The order matches
// dev_all.sh so test runs and dev runs agree:
//
//	1. explicit RUSHES_FFMPEG_FULL_BIN override (CI / sandboxed setups). When set,
//	   it is authoritative — the function never falls back to Homebrew. A typo'd
//	   override therefore surfaces as "not found" rather than silently resolving
//	   to a different binary;
//	2. Homebrew keg-only path via `brew --prefix ffmpeg-full`;
//	3. common layout for Homebrew on Apple Silicon under /opt/homebrew.
func lookupFFmpegFull() (string, bool) {
	if override := strings.TrimSpace(os.Getenv("RUSHES_FFMPEG_FULL_BIN")); override != "" {
		if isExecutable(filepath.Join(override, "ffmpeg")) {
			return override, true
		}
		return "", false
	}
	for _, prefix := range homebrewPrefixes() {
		bin := filepath.Join(prefix, "opt", "ffmpeg-full", "bin")
		if isExecutable(filepath.Join(bin, "ffmpeg")) {
			return bin, true
		}
	}
	return "", false
}

// homebrewPrefixes returns the directories that may host a Homebrew install.
// The list intentionally stays small and only covers the layouts the project
// documents; do not extend it to arbitrary paths.
func homebrewPrefixes() []string {
	var prefixes []string
	if out, err := exec.Command("brew", "--prefix").Output(); err == nil {
		prefixes = append(prefixes, strings.TrimSpace(string(out)))
	}
	prefixes = append(prefixes, "/opt/homebrew", "/usr/local")
	return prefixes
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// prependPath inserts the directory at the front of PATH. The environment is
// inherited by every child process spawned by tests (exec.Command, etc.), so
// `ffmpeg` resolves to the ffmpeg-full binary everywhere downstream.
func prependPath(dir string) {
	current := os.Getenv("PATH")
	if current == "" {
		_ = os.Setenv("PATH", dir)
		return
	}
	segments := filepath.SplitList(current)
	for _, segment := range segments {
		if segment == dir {
			return
		}
	}
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+current)
}
