package store

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// HTTPPublisher publishes a button to the hosted registry over POST /v1/buttons —
// the write-side mirror of HTTPSource. It tars the bundle, hashes the tarball, and
// uploads it bearer-authed with the *write* key (REGISTRY_WRITE_KEY). The registry
// re-verifies the hash, stores the artifact content-addressed under the same key
// scheme HTTPSource downloads from, and appends the index entry. It satisfies
// Publisher, so cmd/publish selects it exactly like install selects HTTPSource.
type HTTPPublisher struct {
	BaseURL string       // the registry base URL ($BUTTONS_REGISTRY_URL), trailing / trimmed
	Key     string       // write bearer key (REGISTRY_WRITE_KEY / BUTTONS_BAT_REGISTRY_WRITE_KEY)
	Kind    string       // "button" | "drawer" (index metadata); "" → "button"
	Client  *http.Client // nil → a 30s-timeout default
}

func (p *HTTPPublisher) httpClient() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Publish uploads a bundle to the registry. Bundle.Name carries the registry
// identity (@desk/name); Version is required — the registry pins immutable
// versions and rejects a re-publish of the same name@version.
func (p *HTTPPublisher) Publish(b *Bundle) error {
	if b.Name == "" || b.Version == "" {
		return fmt.Errorf("registry publish requires name and version (got name=%q version=%q)", b.Name, b.Version)
	}
	tarball, err := tarGz(path.Base(b.Name), b.Files)
	if err != nil {
		return fmt.Errorf("pack %s: %w", b.Name, err)
	}
	if int64(len(tarball)) > maxArtifactBytes {
		return fmt.Errorf("artifact for %s exceeds %d bytes", b.Name, int64(maxArtifactBytes))
	}
	sum := sha256hex(tarball)

	kind := b.Kind
	if kind == "" {
		kind = p.Kind
	}
	if kind == "" {
		kind = "button"
	}
	endpoint := strings.TrimRight(p.BaseURL, "/") +
		fmt.Sprintf("/v1/buttons/%s/%s", scopedPath(b.Name), url.PathEscape(b.Version))
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(tarball))
	if err != nil {
		return err
	}
	if p.Key != "" {
		req.Header.Set("Authorization", "Bearer "+p.Key)
	}
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Content-Sha256", sum) // server verifies bytes against this
	req.Header.Set("X-Button-Kind", kind)
	req.ContentLength = int64(len(tarball))

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("registry %s: %w", p.BaseURL, err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return registryError("publish "+b.Name+"@"+b.Version, resp)
	}
	_ = resp.Body.Close()
	return nil
}

// tarGz packs files into a gzip'd tar wrapped under a single top-level folder,
// mirroring the layout HTTPSource.untarGz expects (it strips the wrapper). Keys
// are sorted and timestamps zeroed so identical content yields an identical
// archive — and so the tarball hash this computes round-trips through the
// registry to install.
func tarGz(wrapper string, files map[string][]byte) ([]byte, error) {
	if wrapper == "" || strings.ContainsAny(wrapper, `/\`) {
		wrapper = "button"
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		data := files[n]
		hdr := &tar.Header{
			Name:     path.Join(wrapper, n),
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  time.Unix(0, 0).UTC(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
