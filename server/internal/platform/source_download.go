package platform

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type sourceDownloadDeps struct {
	lookup  func(context.Context, string) ([]net.IPAddr, error)
	dial    func(context.Context, string, string) (net.Conn, error)
	roots   *x509.CertPool
	tempDir string
}
type sourceDownloader struct {
	hosts map[string]bool
	deps  sourceDownloadDeps
}

var sourceOSSHost = regexp.MustCompile(`^(?:[a-z0-9][a-z0-9-]*\.)?oss-[a-z0-9-]+\.aliyuncs\.com$`)

func newSourceDownloader(hosts string, test *sourceDownloadDeps) (*sourceDownloader, error) {
	d := &sourceDownloader{hosts: map[string]bool{}, deps: sourceDownloadDeps{lookup: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}}
	if hosts == "" {
		return nil, si.ErrUnconfigured
	}
	for _, h := range strings.Split(hosts, ",") {
		h = strings.TrimSpace(h)
		if !sourceOSSHost.MatchString(h) || d.hosts[h] {
			return nil, si.ErrUnconfigured
		}
		d.hosts[h] = true
	}
	// Only this private constructor can supply test networking/trust. Runtime
	// assembly always passes nil; no config can disable origin/peer validation.
	if test != nil {
		if test.lookup != nil {
			d.deps.lookup = test.lookup
		}
		if test.dial != nil {
			d.deps.dial = test.dial
		}
		d.deps.roots = test.roots
		d.deps.tempDir = test.tempDir
	}
	return d, nil
}

var sourceNonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001:10::/28"), netip.MustParsePrefix("2001:20::/28"), netip.MustParsePrefix("2001::/32"), netip.MustParsePrefix("2002::/16"),
}

func sourcePublicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() {
		return false
	}
	for _, p := range sourceNonPublicPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
func (d *sourceDownloader) origin(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Fragment != "" || u.Host == "" || u.Hostname() == "" || u.Port() != "" && u.Port() != "443" || net.ParseIP(u.Hostname()) != nil {
		return nil, si.ErrInvariant
	}
	h := u.Hostname()
	if !d.hosts[h] || u.Host != h && u.Host != h+":443" {
		return nil, si.ErrInvariant
	}
	return u, nil
}

type sourceTempReader struct {
	file *os.File
	once sync.Once
	mu   sync.Mutex
	stop func() bool
	err  error
}

func (r *sourceTempReader) Read(p []byte) (int, error) { return r.file.Read(p) }
func (r *sourceTempReader) Close() error {
	r.once.Do(func() {
		r.mu.Lock()
		if r.stop != nil {
			r.stop()
		}
		r.mu.Unlock()
		r.err = r.file.Close()
		if e := os.Remove(r.file.Name()); r.err == nil && !errors.Is(e, os.ErrNotExist) {
			r.err = e
		}
	})
	return r.err
}
func (d *sourceDownloader) fetch(ctx, lifetime context.Context, raw string, o si.Output) (io.ReadCloser, error) {
	if d == nil {
		return nil, si.ErrUnconfigured
	}
	u, e := d.origin(raw)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ips, e := d.deps.lookup(ctx, u.Hostname())
	if e != nil {
		return nil, si.ErrUnavailable
	}
	if len(ips) == 0 || len(ips) > 16 {
		return nil, si.ErrInvariant
	}
	for _, ip := range ips {
		if ip.Zone != "" || !sourcePublicIP(ip.IP) {
			return nil, si.ErrInvariant
		}
	}
	pin := ips[0].IP
	transport := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, ResponseHeaderTimeout: 5 * time.Second, TLSHandshakeTimeout: 5 * time.Second, MaxResponseHeaderBytes: 32 << 10, TLSClientConfig: &tls.Config{ServerName: u.Hostname(), RootCAs: d.deps.roots, MinVersion: tls.VersionTLS12}}
	transport.DialContext = func(call context.Context, network, address string) (net.Conn, error) {
		if address != net.JoinHostPort(u.Hostname(), "443") || network != "tcp" {
			return nil, si.ErrInvariant
		}
		conn, e := d.deps.dial(call, "tcp", net.JoinHostPort(pin.String(), "443"))
		if e != nil {
			return nil, si.ErrUnavailable
		}
		peer, _, e := net.SplitHostPort(conn.RemoteAddr().String())
		if e != nil || net.ParseIP(peer) == nil || !net.ParseIP(peer).Equal(pin) || !sourcePublicIP(net.ParseIP(peer)) {
			conn.Close()
			return nil, si.ErrInvariant
		}
		return conn, nil
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return nil, si.ErrInvariant
	}
	req.Header.Set("Accept", "image/png")
	response, e := client.Do(req)
	if e != nil {
		return nil, si.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, si.ErrUnavailable
	}
	media, params, e := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if e != nil || media != "image/png" || len(params) != 0 {
		return nil, si.ErrInvariant
	}
	if response.TLS == nil || response.TLS.ServerName != u.Hostname() || len(response.TLS.VerifiedChains) == 0 || response.ContentLength >= 0 && response.ContentLength != o.SizeBytes || o.ContentType != "image/png" || o.SizeBytes <= 0 || o.SizeBytes > 16<<20 || !sourceSHA(o.SHA256) {
		return nil, si.ErrInvariant
	}
	file, e := os.CreateTemp(d.deps.tempDir, "product-source-*")
	if e != nil {
		return nil, si.ErrUnavailable
	}
	reader := &sourceTempReader{file: file}
	success := false
	defer func() {
		if !success {
			reader.Close()
		}
	}()
	if file.Chmod(0600) != nil {
		return nil, si.ErrUnavailable
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	size, e := io.CopyBuffer(io.MultiWriter(file, hash), io.LimitReader(response.Body, (16<<20)+1), buffer)
	if e != nil || ctx.Err() != nil {
		return nil, si.ErrUnavailable
	}
	if size != o.SizeBytes || size > 16<<20 || hex.EncodeToString(hash.Sum(nil)) != o.SHA256 {
		return nil, si.ErrInvariant
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		return nil, si.ErrUnavailable
	}
	n, e := file.Read(buffer[:512])
	if e != nil && e != io.EOF {
		return nil, si.ErrInvariant
	}
	if http.DetectContentType(buffer[:n]) != "image/png" {
		return nil, si.ErrInvariant
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		return nil, si.ErrUnavailable
	}
	config, e := png.DecodeConfig(file)
	if e != nil || config.Width != 1024 || config.Height != 1024 {
		return nil, si.ErrInvariant
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil || lifetime.Err() != nil {
		return nil, si.ErrUnavailable
	}
	reader.mu.Lock()
	reader.stop = context.AfterFunc(lifetime, func() { reader.Close() })
	reader.mu.Unlock()
	if lifetime.Err() != nil {
		return nil, si.ErrUnavailable
	}
	success = true
	return reader, nil
}
func (c *sourceAssetClient) Download(ctx context.Context, r si.Run) (io.ReadCloser, error) {
	if r.Deleted || r.Output == nil || si.ValidateReadyOutput(r, *r.Output) != nil {
		return nil, si.ErrInvariant
	}
	if c.download == nil {
		return nil, si.ErrUnconfigured
	}
	network, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	verified, e := c.Verify(network, r)
	if e != nil {
		return nil, e
	}
	if verified != *r.Output {
		return nil, si.ErrInvariant
	}
	grant, e := c.downloadGrant(network, r)
	if e != nil {
		return nil, e
	}
	return c.download.fetch(network, ctx, grant, *r.Output)
}
