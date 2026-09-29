package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookupFFmpegFullFindsExistingBinary(t *testing.T) {
	if !isExecutable("/opt/homebrew/opt/ffmpeg-full/bin/ffmpeg") {
		t.Skip("ffmpeg-full not installed at /opt/homebrew/opt/ffmpeg-full/bin")
	}
	got, ok := lookupFFmpegFull()
	if !ok {
		t.Fatal("expected lookupFFmpegFull to find ffmpeg-full on this machine")
	}
	if !strings.HasSuffix(got, "/ffmpeg-full/bin") {
		t.Errorf("got=%q does not end with /ffmpeg-full/bin", got)
	}
}

func TestLookupFFmpegFullHonoursOverride(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSHES_FFMPEG_FULL_BIN", dir)
	got, ok := lookupFFmpegFull()
	if !ok {
		t.Fatal("expected override to win")
	}
	if got != dir {
		t.Errorf("got=%q want %q", got, dir)
	}
}

func TestLookupFFmpegFullRejectsMissingBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RUSHES_FFMPEG_FULL_BIN", dir)
	if _, ok := lookupFFmpegFull(); ok {
		t.Fatal("expected lookup to fail when ffmpeg is not present")
	}
}

func TestPrependPathInsertsAtFrontAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "/usr/bin:/bin")
	prependPath(dir)
	first := strings.SplitN(os.Getenv("PATH"), string(os.PathListSeparator), 2)[0]
	if first != dir {
		t.Fatalf("first PATH segment=%q want=%q", first, dir)
	}
	// Re-prepending must not duplicate the directory.
	prependPath(dir)
	segments := filepath.SplitList(os.Getenv("PATH"))
	count := 0
	for _, segment := range segments {
		if segment == dir {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dir appears %d times in PATH=%q", count, os.Getenv("PATH"))
	}
}

func TestPrependPathHandlesEmptyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", "")
	prependPath(dir)
	if got := os.Getenv("PATH"); got != dir {
		t.Fatalf("PATH=%q want=%q", got, dir)
	}
}
