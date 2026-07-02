package store

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// HTTPSource installs from the hosted registry (#275) — a Cloudflare Worker that
// serves the index + content-pinned tarballs over bearer auth. It satisfies
// Source, so the install/update path is identical to a LocalSource; the only
// difference is the bytes come over HTTPS and are hash-verified on the way in.
type HTTPSource struct {
	BaseURL string       // the registry base URL ($BUTTONS_REGISTRY_URL), trailing / trimmed
	Key     string       // bearer key (the REGISTRY_KEY battery / BUTTONS_BAT_REGISTRY_KEY)
	Client  *http.Client // nil → a 30s-timeout default
	Context context.Context
}

// Caps on a fetched artifact — defend against a hostile or oversized registry.
const (
	maxArtifactBytes = 64 << 20  // 64 MiB compressed
	maxBundleBytes   = 256 << 20 // 256 MiB uncompressed
	maxBundleFiles   = 4096
)

func (s *HTTPSource) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (s *HTTPSource) get(p string) (*http.Response, error) {
	ctx := s.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.BaseURL, "/")+p, nil)
	if err != nil {
		return nil, err
	}
	if s.Key != "" {
		req.Header.Set("Authorization", "Bearer "+s.Key)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry %s: %w", s.BaseURL, err)
	}
	return resp, nil
}

// registryError turns a non-200 into a useful message, parsing the
// {"error":{"code","message"}} envelope the Worker returns.
func registryError(what string, resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		return &registryResponseError{what: what, statusCode: resp.StatusCode, code: env.Error.Code, message: env.Error.Message}
	}
	return &registryResponseError{what: what, statusCode: resp.StatusCode, message: strings.TrimSpace(string(body))}
}

type registryResponseError struct {
	what       string
	statusCode int
	code       string
	message    string
}

func (e *registryResponseError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("registry %s: %d %s (%s)", e.what, e.statusCode, e.message, e.code)
	}
	return fmt.Sprintf("registry %s: %d %s", e.what, e.statusCode, e.message)
}

func isVersionExists(err error) bool {
	var regErr *registryResponseError
	return errors.As(err, &regErr) && regErr.statusCode == http.StatusConflict && regErr.code == "VERSION_EXISTS"
}

// indexEntry is one row of the Worker's /v1/index (a superset of ButtonRef).
type indexEntry struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Version string   `json:"version"`
	Tags    []string `json:"tags"`
	SHA256  string   `json:"sha256"`
}

func (s *HTTPSource) index() ([]indexEntry, error) {
	resp, err := s.get("/v1/index")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, registryError("index", resp)
	}
	defer func() { _ = resp.Body.Close() }()
	var entries []indexEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("registry index: decode: %w", err)
	}
	return entries, nil
}

// Index returns the catalog (for search / dependency resolution).
func (s *HTTPSource) Index() ([]ButtonRef, error) {
	entries, err := s.index()
	if err != nil {
		return nil, err
	}
	refs := make([]ButtonRef, 0, len(entries))
	for _, e := range entries {
		refs = append(refs, ButtonRef{Name: e.Name, Kind: e.Kind, Version: e.Version, Tags: e.Tags, SHA256: e.SHA256})
	}
	return refs, nil
}

// Fetch downloads a package tarball, verifies it against the registry's advertised
// content hash, and extracts it into a Bundle. version "" resolves to latest.
func (s *HTTPSource) Fetch(name, version string) (*Bundle, error) {
	// Resolve "" → latest. The Worker treats the last index entry for a name as
	// latest; mirror that so install/update agree with the server.
	wantHash := ""
	if version == "" {
		entries, err := s.index()
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Name == name {
				version, wantHash = e.Version, e.SHA256
			}
		}
		if version == "" {
			return nil, fmt.Errorf("package %q not found in registry", name)
		}
	}

	p := fmt.Sprintf("/v1/buttons/%s/%s/download", scopedPath(name), url.PathEscape(version))
	resp, err := s.get(p)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, registryError("download "+name+"@"+version, resp)
	}
	defer func() { _ = resp.Body.Close() }()
	tarball, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("package %q: download: %w", name, err)
	}
	if int64(len(tarball)) > maxArtifactBytes {
		return nil, fmt.Errorf("package %q: artifact exceeds %d bytes", name, int64(maxArtifactBytes))
	}

	// Integrity: the bytes must match the registry's advertised tarball hash —
	// from the x-content-sha256 header and/or the resolved index entry.
	got := sha256hex(tarball)
	want := resp.Header.Get("X-Content-Sha256")
	if want == "" {
		want = wantHash
	}
	if want != "" && got != want {
		return nil, fmt.Errorf("package %q@%s: content hash mismatch (registry %s, got %s)", name, version, want, got)
	}

	files, err := untarGz(tarball)
	if err != nil {
		return nil, fmt.Errorf("package %q: %w", name, err)
	}
	kind := "button"
	if _, ok := files["button.json"]; !ok {
		if _, hasDrawer := files["drawer.json"]; !hasDrawer {
			return nil, fmt.Errorf("package %q: bundle has no button.json or drawer.json", name)
		}
		kind = "drawer"
	}
	// Bundle.SHA256 is the file-content hash (what install stamps as content_hash),
	// consistent with LocalSource — distinct from the tarball hash verified above.
	return &Bundle{Name: name, Kind: kind, Version: version, SHA256: hashFiles(files), Files: files}, nil
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// scopedPath renders a registry name (@desk/name) as path segments — each
// component percent-escaped, the scope slash preserved — so the URL is
// /v1/buttons/@desk/name (two real segments), not an opaque @desk%2Fname.
func scopedPath(name string) string {
	parts := strings.Split(name, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// untarGz extracts a gzip'd tar into a flat map keyed by path relative to the
// single top-level folder (publish.mjs wraps each button as <name>/...). Directory
// and non-regular entries are skipped; absolute / traversal paths are rejected;
// safeJoin at write time is the final backstop.
func untarGz(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("untar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // dirs, symlinks, etc.
		}
		clean, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return nil, fmt.Errorf("unsafe path in artifact: %q", hdr.Name)
		}
		i := strings.IndexByte(clean, '/')
		if i < 0 {
			continue // a bare top-level file (no wrapping button dir) — skip
		}
		rel := clean[i+1:]
		if rel == "" || strings.HasPrefix(rel, "pressed/") {
			continue // skip the wrapper itself and local run history
		}
		rel, err = cleanArchivePath(rel)
		if err != nil {
			return nil, fmt.Errorf("unsafe path in artifact: %q", hdr.Name)
		}
		if base := path.Base(rel); strings.HasPrefix(base, "._") || base == ".DS_Store" {
			continue // macOS AppleDouble / Finder junk — never part of a button
		}
		if len(files) >= maxBundleFiles {
			return nil, fmt.Errorf("artifact has too many files (> %d)", maxBundleFiles)
		}
		buf, err := io.ReadAll(io.LimitReader(tr, maxBundleBytes-total+1))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		total += int64(len(buf))
		if total > maxBundleBytes {
			return nil, fmt.Errorf("artifact too large uncompressed (> %d bytes)", int64(maxBundleBytes))
		}
		files[rel] = buf
	}
	return files, nil
}

func cleanArchivePath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("path traversal")
		}
	}
	clean := path.Clean(name)
	if clean == "." || !fs.ValidPath(clean) {
		return "", fmt.Errorf("invalid path")
	}
	local, err := filepath.Localize(clean)
	if err != nil {
		return "", err
	}
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("non-local path")
	}
	return filepath.ToSlash(local), nil
}
