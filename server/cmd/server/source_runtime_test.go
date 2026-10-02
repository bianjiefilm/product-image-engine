package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples/sampletest"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

func sourceRuntimeConfig() config.Config {
	return config.Config{AppID: "product-image", IdentityAppID: "product-image", IdentityBaseURL: "http://127.0.0.1:18101", BillingBaseURL: "http://127.0.0.1:18102", TaskBaseURL: "http://127.0.0.1:18103", UploadBaseURL: "http://127.0.0.1:18104", IdentityToken: "fixture-identity", BillingToken: "fixture-billing", TaskToken: "fixture-task", UploadToken: "fixture-upload", SourceDownloadHosts: "fixture.oss-cn-hangzhou.aliyuncs.com", SourcePricingVersion: "fixture-price-v1", GenerationEnabled: true, BillingEnabled: true, SourceImagesEnabled: true}
}
func sourceRuntimeStore(t *testing.T) *store.Store {
	t.Helper()
	st, e := store.Open(filepath.Join(t.TempDir(), "source-runtime.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
func TestSourceRuntimeDefaultsFailClosed(t *testing.T) {
	st := sourceRuntimeStore(t)
	svc, e := newSourceRuntime(config.Config{}, st)
	if svc != nil || !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(svc, e)
	}
	rows, e := st.ListDueSourceRuns(t.Context(), 1, 10)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func TestSourceRuntimeFormalPortsShareOnlyOwnedStore(t *testing.T) {
	st := sourceRuntimeStore(t)
	c := sourceRuntimeConfig()
	svc, e := newSourceRuntime(c, st)
	if e != nil || svc == nil {
		t.Fatal(svc, e)
	}
	if svc.Store != st || svc.Auth == nil || svc.Auth.Store != st || svc.Auth.AppID != c.AppID || svc.Bill == nil || svc.Tasks == nil || svc.Assets == nil || svc.Auth.Context == nil {
		t.Fatal("incomplete formal assembly", svc)
	}
	q, confirm, ready := svc.CreationCapabilities("personal")
	if !q || !confirm || ready || svc.OutputRecoveryReady {
		t.Fatal("assembly prematurely advertised joined recovery", q, confirm, ready)
	}
}
func TestSourceRuntimeCreationOffStillHasReadRecoveryClients(t *testing.T) {
	for _, field := range []string{"source", "billing", "generation", "price", "hosts"} {
		t.Run(field, func(t *testing.T) {
			c := sourceRuntimeConfig()
			switch field {
			case "source":
				c.SourceImagesEnabled = false
			case "billing":
				c.BillingEnabled = false
			case "generation":
				c.GenerationEnabled = false
			case "price":
				c.SourcePricingVersion = ""
			case "hosts":
				c.SourceDownloadHosts = ""
			}
			svc, e := newSourceRuntime(c, sourceRuntimeStore(t))
			if e != nil || svc == nil || svc.Bill == nil || svc.Tasks == nil || svc.Assets == nil || svc.Auth.Context == nil {
				t.Fatal("creation flag blocked historical recovery", svc, e)
			}
			q, confirm, ready := svc.CreationCapabilities("personal")
			if q || confirm || ready {
				t.Fatal("unconfigured creation enabled", q, confirm, ready)
			}
			if field == "hosts" {
				if _, e = svc.Assets.Verify(context.Background(), si.Run{}); !errors.Is(e, si.ErrUnconfigured) {
					t.Fatal("hostless Verify not fail closed", e)
				}
				if body, e := svc.Assets.Download(context.Background(), si.Run{}); body != nil || !errors.Is(e, si.ErrUnconfigured) {
					t.Fatal(body, e)
				}
			}
		})
	}
}
func TestSourceRuntimeInvalidCredentialsAndHostsDenied(t *testing.T) {
	for _, field := range []string{"app", "duplicate", "missing", "base", "host", "hostURL", "hostDuplicate"} {
		t.Run(field, func(t *testing.T) {
			c := sourceRuntimeConfig()
			switch field {
			case "app":
				c.IdentityAppID = "foreign"
			case "duplicate":
				c.UploadToken = c.TaskToken
			case "missing":
				c.BillingToken = ""
			case "base":
				c.TaskBaseURL = ""
			case "host":
				c.SourceDownloadHosts = "example.com"
			case "hostURL":
				c.SourceDownloadHosts = "https://fixture.oss-cn-hangzhou.aliyuncs.com"
			case "hostDuplicate":
				c.SourceDownloadHosts += " ," + c.SourceDownloadHosts
			}
			svc, e := newSourceRuntime(c, sourceRuntimeStore(t))
			if svc != nil || !errors.Is(e, si.ErrUnconfigured) {
				t.Fatal("invalid assembly accepted", svc, e)
			}
		})
	}
}

func TestSourceRuntimePublishedOperationsFailClosed(t *testing.T) {
	if !sourceOperationsAvailable(sourceRequiredOperations) || len(sourceRequiredOperations) != 14 {
		t.Fatal("published contract incomplete")
	}
	if sourceOperationsAvailable(append(append([]string{}, sourceRequiredOperations...), "task.fixture_unpublished")) {
		t.Fatal("unpublished operation accepted")
	}
}

type sourceRuntimeTransport struct {
	calls   int
	request *http.Request
}

func (p *sourceRuntimeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p.calls++
	p.request = r
	return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":"unavailable"}`)), Request: r}, nil
}
func TestSourceRuntimeDefaultAndConstructionDoNotCallAuthority(t *testing.T) {
	tr := &sourceRuntimeTransport{}
	st := sourceRuntimeStore(t)
	if svc, e := sourceRuntimeWithHTTP(config.Config{}, st, &http.Client{Transport: tr}); svc != nil || !errors.Is(e, si.ErrUnconfigured) {
		t.Fatal(svc, e)
	}
	c := sourceRuntimeConfig()
	svc, e := sourceRuntimeWithHTTP(c, st, &http.Client{Transport: tr})
	if e != nil {
		t.Fatal(e)
	}
	if tr.calls != 0 {
		t.Fatal("assembly mutated authority", tr.calls)
	}
	_, e = svc.Auth.Context.Resolve(t.Context(), si.ContextQuery{AppID: c.AppID, UserID: "usr-a", ReadOnly: true})
	if e == nil || tr.calls != 1 || tr.request.URL.Host != "127.0.0.1:18101" || tr.request.URL.Path != "/internal/v1/identity/context" || tr.request.Header.Get(pc.HeaderToken) != c.IdentityToken {
		t.Fatal("not the formal app scoped SDK", tr.calls, tr.request, e)
	}
}

func TestSourceRuntimeRejectsUnsafeTransportBases(t *testing.T) {
	for _, base := range []string{"ftp://127.0.0.1:18101", "file://localhost/tmp", "//localhost:18101", "/relative", "http://fixture:secret@localhost:18101", "http://localhost:18101?private=1", "http://localhost:18101#private", "http://example.com:18101", "http://localhost:bad"} {
		t.Run(base, func(t *testing.T) {
			c := sourceRuntimeConfig()
			c.IdentityBaseURL = base
			svc, e := newSourceRuntime(c, sourceRuntimeStore(t))
			if svc != nil || !errors.Is(e, si.ErrUnconfigured) {
				t.Fatal("unsafe transport base constructed", base, svc, e)
			}
		})
	}
}
func TestSourceRuntimePreservesSDKLoopbackPrefixPath(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:18101/prefix/", "https://localhost:18101/prefix/", "http://[::1]:18101/prefix/"} {
		t.Run(base, func(t *testing.T) {
			c := sourceRuntimeConfig()
			c.IdentityBaseURL = base
			tr := &sourceRuntimeTransport{}
			svc, e := sourceRuntimeWithHTTP(c, sourceRuntimeStore(t), &http.Client{Transport: tr})
			if e != nil {
				t.Fatal(e)
			}
			if tr.calls != 0 {
				t.Fatal("configuration probe sent")
			}
			_, _ = svc.Auth.Context.Resolve(t.Context(), si.ContextQuery{AppID: c.AppID, UserID: "usr-a", ReadOnly: true})
			if tr.calls != 1 || tr.request.URL.String() != strings.TrimRight(base, "/")+"/internal/v1/identity/context" {
				t.Fatal("SDK prefix path changed", tr.calls, tr.request)
			}
		})
	}
}

func TestSourceRuntimeJoinedStartupReadinessAndCancellation(t *testing.T) {
	for _, hosts := range []bool{true, false} {
		t.Run(fmt.Sprint(hosts), func(t *testing.T) {
			c := sourceRuntimeConfig()
			if !hosts {
				c.SourceDownloadHosts = ""
			}
			st := sourceRuntimeStore(t)
			svc, e := newSourceRuntime(c, st)
			if e != nil {
				t.Fatal(e)
			}
			co, e := startSourceCoordinator(t.Context(), svc)
			if e != nil || co == nil {
				t.Fatal("worker did not register", co, e)
			}
			if svc.OutputRecoveryReady != hosts {
				t.Fatal("readiness set without joined output assembly", svc.OutputRecoveryReady, hosts)
			}
			co.cancel()
			select {
			case <-co.done:
				if !errors.Is(co.err, context.Canceled) {
					t.Fatal(co.err)
				}
			case <-time.After(time.Second):
				t.Fatal("worker did not join")
			}
			if _, e = st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10); e != nil {
				t.Fatal("worker closed shared store", e)
			}
		})
	}
}

func TestSourceRuntimeShutdownTimeoutKeepsSharedStoreOpen(t *testing.T) {
	for _, blocked := range []string{"request", "worker"} {
		t.Run(blocked, func(t *testing.T) {
			st := sourceRuntimeStore(t)
			root, cancel := context.WithCancel(t.Context())
			defer cancel()
			release := make(chan struct{})
			entered := make(chan struct{})
			requestDone := make(chan struct{})
			requests := newSourceRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; close(requestDone) }))
			co := &sourceCoordinator{cancel: func() {}, done: make(chan struct{})}
			if blocked == "request" {
				close(co.done)
				go requests.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil).WithContext(root))
				<-entered
			} else {
				close(requestDone)
			}
			ctx, deadlineCancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			defer deadlineCancel()
			e := stopSourceProcess(ctx, cancel, &http.Server{}, requests, co, st)
			_, dbError := st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10)
			close(release)
			if blocked == "worker" {
				close(co.done)
			}
			select {
			case <-requestDone:
			case <-time.After(time.Second):
				t.Fatal("test request failed to join")
			}
			if !errors.Is(e, errSourceUnjoined) || dbError != nil {
				t.Fatal("unjoined shutdown closed database or claimed success", e, dbError)
			}
		})
	}
}
func TestSourceRuntimeShutdownCancelsThenJoinsBeforeClose(t *testing.T) {
	st := sourceRuntimeStore(t)
	root, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := sourceRuntimeConfig()
	svc, e := newSourceRuntime(c, st)
	if e != nil {
		t.Fatal(e)
	}
	co, e := startSourceCoordinator(root, svc)
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	checked := make(chan error, 1)
	requests := newSourceRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		_, e := st.ListDueSourceRuns(context.Background(), time.Now().Unix(), 10)
		checked <- e
	}))
	go requests.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil).WithContext(root))
	<-entered
	ctx, deadlineCancel := context.WithTimeout(t.Context(), time.Second)
	defer deadlineCancel()
	e = stopSourceProcess(ctx, cancel, &http.Server{}, requests, co, st)
	select {
	case dbError := <-checked:
		if dbError != nil {
			t.Fatal("closed while request still using DB", dbError)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("request not canceled")
	}
	if e != nil {
		t.Fatal(e)
	}
	if _, e = st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10); e == nil {
		t.Fatal("store not closed after complete join")
	}
}

func TestSourceRuntimeUnexpectedCoordinatorExitStopsServing(t *testing.T) {
	st := sourceRuntimeStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	co := &sourceCoordinator{cancel: func() {}, done: make(chan struct{}), err: errors.New("fixture database failure")}
	close(co.done)
	if e := waitSourceStop(ctx, make(chan error), co); !errors.Is(e, errSourceCoordinatorExited) {
		t.Fatal("failed worker left HTTP falsely ready", e)
	}
	requests := newSourceRequests(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("accepted work after shutdown") }))
	bound, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if e := stopSourceProcess(bound, cancel, &http.Server{}, requests, co, st); e != nil {
		t.Fatal(e)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("service request context not canceled")
	}
	w := httptest.NewRecorder()
	requests.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 503 {
		t.Fatal("shutdown admission left open", w.Code)
	}
}
func TestSourceRuntimeDisabledCoordinatorAndNormalSignalExit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	co, e := startSourceCoordinator(ctx, nil)
	if e != nil || co != nil {
		t.Fatal(co, e)
	}
	if e = waitSourceStop(ctx, make(chan error), nil); e != nil {
		t.Fatal("normal cancel fatal", e)
	}
}

type sourceRuntimeBlockingTransport struct {
	st            *store.Store
	entered       chan time.Duration
	dbAfterCancel chan error
}

func (p *sourceRuntimeBlockingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	deadline, ok := r.Context().Deadline()
	if !ok {
		return nil, errors.New("request lacked deadline")
	}
	p.entered <- time.Until(deadline)
	<-r.Context().Done()
	_, e := p.st.ListDueSourceRuns(context.Background(), time.Now().Unix(), 10)
	p.dbAfterCancel <- e
	return nil, r.Context().Err()
}
func TestSourceRuntimeInFlightOriginalSDKReadJoinsBeforeStoreClose(t *testing.T) {
	st := sourceRuntimeStore(t)
	c := sourceRuntimeConfig()
	c.SourceImagesEnabled = false
	intent := si.Intent{Scope: si.Scope{AppID: c.AppID, TenantID: "acct-fixture", ProjectID: "proj-fixture", UserID: "usr-fixture", PrincipalAccountID: "acct-fixture", PayerAccountID: "acct-fixture"}, RequestKey: "runtime-original", Mode: "text_generate", Provider: "modelxing-qwen-image-2.0-v1", Model: "qwen-image-2.0", Capability: "image.generate", Size: "1024*1024", Prompt: "fixture", PricingVersion: "fixture-price-v1", Quantity: 1}
	r, _, e := st.CreateSourceRun(t.Context(), intent)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().Unix()
	q := si.Quote{UsageID: "usage-fixture", UsageKey: r.UsageKey, QuoteID: "quote-fixture", PricingVersion: intent.PricingVersion, BusinessRef: r.BusinessRef, Currency: "CNY", Quantity: 1, UnitPriceMinor: 25, AmountMinor: 25, QuotedAtUnix: now, ExpiresAtUnix: now + 300}
	if _, e = st.SaveSourceQuote(t.Context(), intent.Scope, r.ID, q); e != nil {
		t.Fatal(e)
	}
	if _, e = st.ConfirmSourceRun(t.Context(), intent.Scope, r.ID, si.QuoteHash(q), now); e != nil {
		t.Fatal(e)
	}
	tr := &sourceRuntimeBlockingTransport{st: st, entered: make(chan time.Duration, 1), dbAfterCancel: make(chan error, 1)}
	svc, e := sourceRuntimeWithHTTP(c, st, &http.Client{Transport: tr})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	co, e := startSourceCoordinator(ctx, svc)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case bound := <-tr.entered:
		if bound <= 0 || bound > 20*time.Second {
			t.Fatal(bound)
		}
	case <-time.After(time.Second):
		co.cancel()
		<-co.done
		t.Fatal("original SDK read never entered")
	}
	stopCtx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	if e = stopSourceProcess(stopCtx, cancel, &http.Server{}, newSourceRequests(http.NotFoundHandler()), co, st); e != nil {
		t.Fatal(e)
	}
	if e = <-tr.dbAfterCancel; e != nil {
		t.Fatal("DB closed while original SDK request canceled", e)
	}
	if !errors.Is(co.err, context.Canceled) {
		t.Fatal(co.err)
	}
}

type sourceServeOutcome struct{ stopErr, shutdownErr error }

// This listener owns only net.Pipe connections: no TCP listener, port, TLS,
// provider or external service is created by the real Serve/Shutdown tests.
type sourceMemoryListener struct {
	conn     net.Conn
	accepted sync.Once
	closing  sync.Once
	closed   chan struct{}
}

func (l *sourceMemoryListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.accepted.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *sourceMemoryListener) Close() error   { l.closing.Do(func() { close(l.closed) }); return nil }
func (l *sourceMemoryListener) Addr() net.Addr { return sourceMemoryAddr{} }

type sourceMemoryAddr struct{}

func (sourceMemoryAddr) Network() string { return "pipe" }
func (sourceMemoryAddr) String() string  { return "source-fixture-memory" }
func sourceRuntimeHTTPFixture(t *testing.T, handler http.Handler) (*store.Store, context.Context, context.CancelFunc, *sourceCoordinator, *sourceRequests, *http.Server, net.Listener, net.Conn) {
	t.Helper()
	st := sourceRuntimeStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	svc, e := newSourceRuntime(sourceRuntimeConfig(), st)
	if e != nil {
		t.Fatal(e)
	}
	co, e := startSourceCoordinator(ctx, svc)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cancel()
		co.cancel()
		select {
		case <-co.done:
		case <-time.After(time.Second):
			t.Error("fixture worker did not join")
		}
	})
	reqs := newSourceRequests(handler)
	srv := &http.Server{Handler: reqs, BaseContext: func(net.Listener) context.Context { return ctx }}
	serverConn, clientConn := net.Pipe()
	ln := &sourceMemoryListener{conn: serverConn, closed: make(chan struct{})}
	t.Cleanup(func() { ln.Close(); clientConn.Close(); serverConn.Close() })
	return st, ctx, cancel, co, reqs, srv, ln, clientConn
}
func sourceMemoryRequest(t *testing.T, c net.Conn) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r, _ := http.NewRequest("GET", "http://source-fixture/healthz", nil)
		_ = r.Write(c)
		_, _ = io.Copy(io.Discard, c)
	}()
	return done
}
func sourceAwait(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
func TestSourceRuntimeRealServeUnexpectedWorkerCancelsAndJoins(t *testing.T) {
	entered := make(chan struct{})
	afterCancel := make(chan error, 1)
	release := make(chan struct{})
	var st *store.Store
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		_, e := st.ListDueSourceRuns(context.Background(), time.Now().Unix(), 10)
		afterCancel <- e
		<-release
	})
	var ctx context.Context
	var cancel context.CancelFunc
	var co *sourceCoordinator
	var reqs *sourceRequests
	var srv *http.Server
	var ln net.Listener
	var client net.Conn
	st, ctx, cancel, co, reqs, srv, ln, client = sourceRuntimeHTTPFixture(t, handler)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	done := make(chan sourceServeOutcome, 1)
	go func() {
		stopErr, shutdownErr := serveSourceRuntime(ctx, cancel, srv, reqs, co, st, ln, 2*time.Second)
		done <- sourceServeOutcome{stopErr, shutdownErr}
	}()
	requestDone := sourceMemoryRequest(t, client)
	sourceAwait(t, entered, "real Serve never accepted the in-memory HTTP request")
	co.cancel() // Actual worker completes while parent is live: unexpected exit.
	select {
	case e := <-afterCancel:
		if e != nil {
			t.Fatal("database closed before canceled request joined", e)
		}
	case <-time.After(time.Second):
		t.Fatal("worker exit did not cancel actual HTTP BaseContext")
	}
	sourceAwait(t, co.done, "actual coordinator not joined")
	if _, e := st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10); e != nil {
		t.Fatal("Store closed while HTTP handler still active", e)
	}
	close(release)
	select {
	case e := <-done:
		if e.shutdownErr != nil || !errors.Is(e.stopErr, errSourceCoordinatorExited) {
			t.Fatal("unexpected worker exit hidden", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("actual runtime failed to join")
	}
	sourceAwait(t, requestDone, "HTTP connection not shut down")
	if _, e := st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10); e == nil {
		t.Fatal("fully joined runtime did not close Store")
	}
}
func TestSourceRuntimeRealServeCanceledSignalIsNotFatal(t *testing.T) {
	entered := make(chan struct{})
	checked := make(chan error, 1)
	var st *store.Store
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		_, e := st.ListDueSourceRuns(context.Background(), time.Now().Unix(), 10)
		checked <- e
	})
	st, ctx, cancel, co, reqs, srv, ln, client := sourceRuntimeHTTPFixture(t, handler)
	done := make(chan sourceServeOutcome, 1)
	go func() {
		stopErr, shutdownErr := serveSourceRuntime(ctx, cancel, srv, reqs, co, st, ln, time.Second)
		done <- sourceServeOutcome{stopErr, shutdownErr}
	}()
	requestDone := sourceMemoryRequest(t, client)
	sourceAwait(t, entered, "real Serve did not accept")
	cancel()
	select {
	case e := <-done:
		if e.stopErr != nil || e.shutdownErr != nil {
			t.Fatal("ordinary canceled parent treated as fatal", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled runtime not joined")
	}
	if e := <-checked; e != nil {
		t.Fatal("request lost original Store before join", e)
	}
	sourceAwait(t, requestDone, "ordinary shutdown connection not closed")
	if _, e := st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10); e == nil {
		t.Fatal("Store not closed after ordinary complete join")
	}
}
func TestSourceRuntimeRealServeTimeoutDoesNotCloseStore(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; close(handlerDone) })
	st, ctx, cancel, co, reqs, srv, ln, client := sourceRuntimeHTTPFixture(t, handler)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	done := make(chan sourceServeOutcome, 1)
	go func() {
		stopErr, shutdownErr := serveSourceRuntime(ctx, cancel, srv, reqs, co, st, ln, 40*time.Millisecond)
		done <- sourceServeOutcome{stopErr, shutdownErr}
	}()
	requestDone := sourceMemoryRequest(t, client)
	sourceAwait(t, entered, "real Serve did not accept")
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e.shutdownErr, errSourceUnjoined) {
			t.Fatal("unjoined actual handler claimed success", e)
		}
	case <-time.After(time.Second):
		t.Fatal("actual shutdown not bounded")
	}
	sourceAwait(t, co.done, "worker did not join during handler timeout")
	if _, e := st.ListDueSourceRuns(t.Context(), time.Now().Unix(), 10); e != nil {
		t.Fatal("timeout closed in-use original Store", e)
	}
	close(release)
	sourceAwait(t, handlerDone, "fixture handler not released")
	sourceAwait(t, requestDone, "timeout did not close HTTP connection")
}

func TestPlateLockAssemblyIsOffByDefaultAndFailsClosed(t *testing.T) {
	good := sampletest.Dir(t)
	broken := sampletest.Copy(t)
	if e := os.Remove(filepath.Join(broken, "masks", "carton-1024x1024.png")); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name        string
		flag        bool
		dir         string
		wantEnabled bool
		wantSet     bool
		wantReason  string
	}{
		{"flag off ignores a valid directory", false, good, false, false, ""},
		{"flag on without a directory", true, "", true, false, "sample_set_unconfigured"},
		{"flag on with a broken set", true, broken, true, false, "sample_set_invalid"},
		{"flag on with the frozen set", true, good, true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sourceRuntimeConfig()
			c.BgPlateLockEnabled, c.FidelitySamplesDir = tc.flag, tc.dir
			svc, e := newSourceRuntime(c, sourceRuntimeStore(t))
			if e != nil || svc == nil {
				t.Fatal(svc, e)
			}
			if svc.PlateEnabled != tc.wantEnabled || (svc.Samples != nil) != tc.wantSet || svc.SamplesReason != tc.wantReason {
				t.Fatalf("enabled=%v set=%v reason=%q", svc.PlateEnabled, svc.Samples != nil, svc.SamplesReason)
			}
			if ready, _ := svc.PlateReady("personal"); ready {
				t.Fatal("the joined recovery runtime is not started here, so the mode can never be ready")
			}
		})
	}
}
