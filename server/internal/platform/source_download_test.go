package platform

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"image"
	"image/png"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const sourceDownloadHost = "sample.oss-cn-hangzhou.aliyuncs.com"

type sourcePinnedTestConn struct {
	net.Conn
	peer net.Addr
}

func (c sourcePinnedTestConn) RemoteAddr() net.Addr { return c.peer }

func sourcePNG(t *testing.T, width int) []byte {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, 1024))); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func sourceBytesFact(data []byte) si.Output {
	sum := sha256.Sum256(data)
	return si.Output{ContentType: "image/png", SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data))}
}
func sourceFetchFixture(t *testing.T, handler http.HandlerFunc) (*sourceDownloader, *atomic.Int64) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: sourceDownloadHost}, DNSNames: []string{sourceDownloadHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	calls := &atomic.Int64{}
	deps := &sourceDownloadDeps{roots: roots, tempDir: t.TempDir(), lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		calls.Add(1)
		if address != "8.8.8.8:443" || network != "tcp" {
			return nil, errors.New("unpinned dial")
		}
		conn, e := (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		if e != nil {
			return nil, e
		}
		return sourcePinnedTestConn{Conn: conn, peer: &net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 443}}, nil
	}}
	d, e := newSourceDownloader(sourceDownloadHost, deps)
	if e != nil {
		t.Fatal(e)
	}
	return d, calls
}
func TestSourceDownloadStreamsVerifiedPNGAndRemovesTemp(t *testing.T) {
	data := sourcePNG(t, 1024)
	d, calls := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != sourceDownloadHost || r.TLS.ServerName != sourceDownloadHost {
			t.Error("original host/TLS identity lost")
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	})
	body, e := d.fetch(t.Context(), t.Context(), "https://"+sourceDownloadHost+"/a.png?signature=fixture", sourceBytesFact(data))
	if e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(d.deps.tempDir, "*"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	stat, e := os.Stat(files[0])
	if e != nil || stat.Mode().Perm() != 0600 {
		t.Fatal(stat, e)
	}
	got, e := io.ReadAll(body)
	if e != nil || !bytes.Equal(got, data) || calls.Load() != 1 {
		t.Fatal(e, calls.Load())
	}
	if e = body.Close(); e != nil {
		t.Fatal(e)
	}
	files, _ = filepath.Glob(filepath.Join(d.deps.tempDir, "*"))
	if len(files) != 0 {
		t.Fatal("temp leaked", files)
	}
}
func TestSourceDownloadRejectsUnsafeOriginsAndAllDNSAnswers(t *testing.T) {
	d, calls := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe request sent") })
	for _, raw := range []string{"http://" + sourceDownloadHost + "/x", "https://user@" + sourceDownloadHost + "/x", "https://" + sourceDownloadHost + ":444/x", "https://8.8.8.8/x", "https://" + sourceDownloadHost + "/x#fragment", "https://other.oss-cn-hangzhou.aliyuncs.com/x", "https://" + sourceDownloadHost + "./x", "https://" + sourceDownloadHost + "%2f.evil/x"} {
		if body, e := d.fetch(t.Context(), t.Context(), raw, sourceBytesFact([]byte("x"))); e == nil || body != nil {
			t.Fatal("unsafe origin accepted", raw)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "100.64.0.1", "192.0.2.1", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "64:ff9b::a00:1"} {
		d.deps.lookup = func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP(ip)}}, nil
		}
		if body, e := d.fetch(t.Context(), t.Context(), "https://"+sourceDownloadHost+"/x", sourceBytesFact([]byte("x"))); e == nil || body != nil {
			t.Fatal("unsafe DNS accepted", ip)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe DNS dialed", calls.Load())
	}
}
func TestSourceDownloadRejectsContentFaultsAndCleansErrors(t *testing.T) {
	data := sourcePNG(t, 1024)
	for _, fault := range []string{"redirect", "status", "mime", "length", "hash", "dimensions", "chunked_overlimit", "truncated", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			payload := data
			o := sourceBytesFact(data)
			if fault == "hash" {
				o.SHA256 = strings.Repeat("a", 64)
			}
			if fault == "dimensions" {
				payload = sourcePNG(t, 1023)
				o = sourceBytesFact(payload)
			}
			if fault == "chunked_overlimit" {
				o.SizeBytes = 16 << 20
				payload = make([]byte, (16<<20)+1)
				sum := sha256.Sum256(payload)
				o.SHA256 = hex.EncodeToString(sum[:])
			}
			d, calls := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				switch fault {
				case "redirect":
					w.Header().Set("Location", "https://evil.invalid/x")
					w.WriteHeader(302)
					return
				case "status":
					w.WriteHeader(500)
					return
				case "mime":
					w.Header().Set("Content-Type", "text/plain")
				case "length":
					w.Header().Set("Content-Length", "1")
					w.Write([]byte("x"))
					return
				case "truncated":
					w.Header().Set("Content-Length", "999999")
					w.Write(payload[:10])
					return
				case "cancel":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					return
				case "chunked_overlimit":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				w.Write(payload)
			})
			ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
			defer cancel()
			if body, e := d.fetch(ctx, ctx, "https://"+sourceDownloadHost+"/x", o); e == nil || body != nil {
				t.Fatal("fault accepted", fault)
			}
			files, _ := filepath.Glob(filepath.Join(d.deps.tempDir, "*"))
			if len(files) != 0 || calls.Load() != 1 {
				t.Fatal(files, calls.Load())
			}
		})
	}
}
func TestSourceDownloadCancelRemovesReturnedTemp(t *testing.T) {
	data := sourcePNG(t, 1024)
	d, _ := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	})
	ctx, cancel := context.WithCancel(t.Context())
	body, e := d.fetch(ctx, ctx, "https://"+sourceDownloadHost+"/x", sourceBytesFact(data))
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	body.Close()
	files, _ := filepath.Glob(filepath.Join(d.deps.tempDir, "*"))
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestSourceDownloadRejectsChangedPeerOrUntrustedTLS(t *testing.T) {
	data := sourcePNG(t, 1024)
	for _, fault := range []string{"changed_peer", "untrusted_tls"} {
		t.Run(fault, func(t *testing.T) {
			d, calls := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe peer/TLS request reached HTTP") })
			if fault == "untrusted_tls" {
				d.deps.roots = x509.NewCertPool()
			} else {
				dial := d.deps.dial
				d.deps.dial = func(ctx context.Context, n, a string) (net.Conn, error) {
					c, e := dial(ctx, n, a)
					if e != nil {
						return nil, e
					}
					return sourcePinnedTestConn{Conn: c, peer: &net.TCPAddr{IP: net.ParseIP("1.1.1.1"), Port: 443}}, nil
				}
			}
			if body, e := d.fetch(t.Context(), t.Context(), "https://"+sourceDownloadHost+"/x", sourceBytesFact(data)); e == nil || body != nil {
				t.Fatal("peer/TLS drift accepted")
			}
			if calls.Load() != 1 {
				t.Fatal(calls.Load())
			}
		})
	}
}

type sourceDeadlineTransport struct {
	next     http.RoundTripper
	t        *testing.T
	deadline time.Time
}

func (rt *sourceDeadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	deadline, ok := r.Context().Deadline()
	if !ok || time.Until(deadline) > 15*time.Second || time.Until(deadline) <= 0 {
		rt.t.Error("SDK operation lacks original fifteen-second budget")
	}
	if deadline.After(rt.deadline) {
		rt.t.Error("operation extended the complete download budget")
	}
	return rt.next.RoundTrip(r)
}

func TestSourceDownloadFreshProofGrantFetchShareOneDeadlineAndNoProxy(t *testing.T) {
	data := sourcePNG(t, 1024)
	d, dials := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	})
	proxyCalls := &atomic.Int64{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(500) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	r := sourceWireRun()
	o := sourceBytesFact(data)
	o.AssetID = "asset-a"
	o.ReferenceID = "ref-a"
	o.ProjectID = r.Intent.Scope.ProjectID
	o.AppID = r.Intent.Scope.AppID
	o.PrincipalType = "user"
	o.PrincipalID = r.Intent.Scope.UserID
	o.Status = "ready"
	o.ReferenceState = "active"
	r.TaskID = "task-a"
	r.ConfirmationHash = si.QuoteHash(*r.Quote)
	r.Task = &si.TaskFact{ID: r.TaskID, IdempotencyKey: r.TaskKey, Scope: r.Intent.Scope, Quote: *r.Quote, Provider: r.Intent.Provider, Capability: r.Intent.Capability, Phase: "succeeded", Status: "succeeded", HoldID: "hold-a", ChargeID: "charge-a", Output: &o}
	r.Output = &o
	n := int64(25)
	r.Bill = &si.BillFact{Scope: r.Intent.Scope, UsageID: r.Quote.UsageID, UsageKey: r.UsageKey, Capability: r.Intent.Capability, Quantity: 1, PricingVersion: r.Intent.PricingVersion, BusinessRef: r.BusinessRef, Status: "charged", Quote: r.Quote, HoldID: "hold-a", ChargeID: "charge-a", ChargedMinor: &n}
	history := *r.Bill
	r.ChargedBill = &history
	var operations []string
	var mu sync.Mutex
	released := false
	asset := map[string]any{"asset_version": "durable_asset_v1", "app_id": o.AppID, "principal_type": "user", "principal_id": o.PrincipalID, "asset_id": o.AssetID, "filename": "output.png", "content_type": "image/png", "sha256": o.SHA256, "size_bytes": o.SizeBytes, "status": "ready", "active_reference_count": 1}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		operations = append(operations, q.Method+" "+q.URL.Path)
		if q.Header.Get("X-Principal-ID") != o.PrincipalID {
			t.Error("Upload principal lost")
		}
		switch {
		case strings.HasSuffix(q.URL.Path, "/download-url"):
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": asset, "download_url": "https://" + sourceDownloadHost + "/x?signature=fixture", "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339)})
		case strings.Contains(q.URL.Path, "/references/"):
			state := "active"
			if released {
				state = "released"
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "reference": map[string]any{"asset_version": "durable_asset_v1", "app_id": o.AppID, "principal_type": "user", "principal_id": o.PrincipalID, "asset_id": o.AssetID, "project_id": o.ProjectID, "reference_id": o.ReferenceID, "state": state, "created_at": "2026-10-01T00:00:00Z"}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "asset": asset})
		}
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	rt := &sourceDeadlineTransport{next: upstream.Client().Transport, t: t, deadline: deadline}
	clients, e := NewSourceClients(sourceWireConfig(upstream.URL), &http.Client{Transport: rt})
	if e != nil {
		t.Fatal(e)
	}
	clients.Assets.download = d
	lookup := d.deps.lookup
	d.deps.lookup = func(ctx context.Context, h string) ([]net.IPAddr, error) {
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(rt.deadline) {
			t.Error("fetch/DNS extended original deadline")
		}
		return lookup(ctx, h)
	}
	body, e := clients.Assets.Download(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(body)
	body.Close()
	if e != nil || !bytes.Equal(got, data) || proxyCalls.Load() != 0 || dials.Load() != 1 {
		t.Fatal(e, proxyCalls.Load(), dials.Load())
	}
	mu.Lock()
	if len(operations) != 3 || !strings.Contains(operations[0], "GET") || !strings.Contains(operations[1], "/references/") || !strings.HasSuffix(operations[2], "/download-url") {
		t.Fatal("grant fetched before fresh proof", operations)
	}
	operations = nil
	released = true
	mu.Unlock()
	body, e = clients.Assets.Download(ctx, r)
	mu.Lock()
	defer mu.Unlock()
	if e == nil || body != nil || len(operations) != 2 || dials.Load() != 1 {
		t.Fatal("released reference fetched content", e, operations, dials.Load())
	}
}

func TestSourceDownloadHeadersHaveFiveSecondDeadline(t *testing.T) {
	d, _ := sourceFetchFixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	start := time.Now()
	if body, e := d.fetch(t.Context(), t.Context(), "https://"+sourceDownloadHost+"/x", sourceBytesFact([]byte("x"))); e == nil || body != nil {
		t.Fatal("hanging headers accepted")
	}
	elapsed := time.Since(start)
	if elapsed < 4*time.Second || elapsed > 7*time.Second {
		t.Fatal("header timeout is not five seconds", elapsed)
	}
}
func TestSourceDownloadGrantRejectsExpiredOrMalformedTime(t *testing.T) {
	r := sourceWireRun()
	o := si.Output{AssetID: "asset-a", ReferenceID: "ref-a", ProjectID: r.Intent.Scope.ProjectID, AppID: r.Intent.Scope.AppID, PrincipalType: "user", PrincipalID: r.Intent.Scope.UserID, SHA256: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 1024}
	r.Output = &o
	for _, expiry := range []string{"2020-01-01T00:00:00Z", "invalid"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "download_url": "https://" + sourceDownloadHost + "/x", "expires_at": expiry, "asset": map[string]any{"asset_version": "durable_asset_v1", "app_id": o.AppID, "principal_type": "user", "principal_id": o.PrincipalID, "asset_id": o.AssetID, "filename": "x.png", "content_type": "image/png", "sha256": o.SHA256, "size_bytes": o.SizeBytes, "status": "ready", "active_reference_count": 1}})
		}))
		c, e := NewSourceClients(sourceWireConfig(srv.URL), srv.Client())
		if e != nil {
			t.Fatal(e)
		}
		_, e = c.Assets.downloadGrant(t.Context(), r)
		srv.Close()
		if e == nil {
			t.Fatal("stale/malformed grant accepted", expiry)
		}
	}
}

func TestSourceDownloadUnverifiedOutputCannotFetchContent(t *testing.T) {
	cfg := sourceWireConfig("http://127.0.0.1:1")
	cfg.SourceDownloadHosts = "sqlite-back.oss-cn-hangzhou.aliyuncs.com"
	clients, e := NewSourceClients(config.Config(cfg), nil)
	if e != nil {
		t.Fatal(e)
	}
	r := sourceWireRun()
	if body, e := clients.Assets.Download(t.Context(), r); e == nil || body != nil {
		t.Fatal("unverified output download accepted", body, e)
	}
}
