package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testHTTPResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func makeBinaryArchive(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o700, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestApplyBinaryUsesCheckedReleaseSnapshot(t *testing.T) {
	const (
		archiveURL   = "https://downloads.test/buttons.tar.gz"
		checksumsURL = "https://downloads.test/checksums.txt"
	)
	archiveName := archiveNameForPlatform("2.0.0")
	archiveData := makeBinaryArchive(t, "buttons", []byte("#!/bin/sh\necho updated\n"))
	checksumsData := []byte(fmt.Sprintf("%s  %s\n", sha(archiveData), archiveName))
	releaseJSON := []byte(fmt.Sprintf(`{"tag_name":"v2.0.0","assets":[{"name":%q,"browser_download_url":%q},{"name":"checksums.txt","browser_download_url":%q}]}`, archiveName, archiveURL, checksumsURL))

	var releaseCalls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case releasesAPI:
			releaseCalls.Add(1)
			return testHTTPResponse(http.StatusOK, releaseJSON), nil
		case archiveURL:
			return testHTTPResponse(http.StatusOK, archiveData), nil
		case checksumsURL:
			return testHTTPResponse(http.StatusOK, checksumsData), nil
		default:
			return testHTTPResponse(http.StatusNotFound, nil), nil
		}
	})}

	execPath := filepath.Join(t.TempDir(), "buttons")
	if err := os.WriteFile(execPath, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), Options{
		CurrentVersion: "1.0.0",
		Client:         client,
		ExecutablePath: execPath,
		SkipContent:    true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.UpdatedBinary {
		t.Fatalf("UpdatedBinary = false, result = %+v", result)
	}
	if got := releaseCalls.Load(); got != 1 {
		t.Fatalf("release fetch count = %d, want 1", got)
	}
	data, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "#!/bin/sh\necho updated\n" {
		t.Fatalf("binary contents = %q", string(data))
	}
	info, err := os.Stat(execPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("binary mode = %o, want 700", got)
	}
}

func TestApplyBinaryHomebrewUsesPackageManagerWhenOptedIn(t *testing.T) {
	releaseJSON := []byte(`{"tag_name":"v2.0.0","assets":[]}`)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == releasesAPI {
			return testHTTPResponse(http.StatusOK, releaseJSON), nil
		}
		return testHTTPResponse(http.StatusNotFound, nil), nil
	})}

	binDir := filepath.Join(t.TempDir(), "opt", "homebrew", "Cellar", "buttons", "1.0.0", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	execPath := filepath.Join(binDir, "buttons")
	if err := os.WriteFile(execPath, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}

	fakeBin := t.TempDir()
	argvPath := filepath.Join(t.TempDir(), "brew-argv")
	brewPath := filepath.Join(fakeBin, "brew")
	brewScript := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > %q\n", argvPath)
	if err := os.WriteFile(brewPath, []byte(brewScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := Apply(context.Background(), Options{
		CurrentVersion: "1.0.0",
		Client:         client,
		ExecutablePath: execPath,
		SkipContent:    true,
		CLIAutoUpdate:  true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.UpdatedBinary {
		t.Fatalf("UpdatedBinary = false, result = %+v", result)
	}
	if result.Binary == nil || !result.Binary.HomebrewManaged {
		t.Fatalf("HomebrewManaged not reported: %+v", result.Binary)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(argv), "upgrade\nbuttons\n"; got != want {
		t.Fatalf("brew argv = %q, want %q", got, want)
	}
}

func TestApplyBinaryHomebrewUsesPackageManagerWithEnvOptIn(t *testing.T) {
	t.Setenv("BUTTONS_CLI_AUTO_UPDATE", "1")
	if !cliAutoUpdateEnabled(Options{}) {
		t.Fatal("env opt-in should enable CLI auto-update")
	}
}
