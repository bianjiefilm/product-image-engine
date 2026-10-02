package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bianjiefilm/product-image-engine/server/internal/buildinfo"
	"github.com/bianjiefilm/product-image-engine/server/internal/config"
	"github.com/bianjiefilm/product-image-engine/server/internal/fidelitysamples"
	"github.com/bianjiefilm/product-image-engine/server/internal/platelock"
	"github.com/bianjiefilm/product-image-engine/server/internal/platform"
	"github.com/bianjiefilm/product-image-engine/server/internal/sourceflow"
	si "github.com/bianjiefilm/product-image-engine/server/internal/sourceimage"
	"github.com/bianjiefilm/product-image-engine/server/internal/store"
	pc "github.com/bianjiefilm/public-ai/sdk/go/platformconsumer"
)

var sourceRequiredOperations = []string{"identity.context.resolve", "billing.usage", "billing.usage_lookup", "billing.usage_get", "billing.source_quote", "task.source_submit", "task.source_get", "task.source_lookup", "task.source_cancel", "task.source_reconcile", "upload.durable_asset_get", "upload.durable_download", "upload.durable_reference_get", "upload.durable_release"}

func sourceOperationsAvailable(ids []string) bool {
	for _, id := range ids {
		if _, ok, e := pc.Lookup(id); e != nil || !ok {
			return false
		}
	}
	return true
}

// Read/recovery authority is independent of the optional creation flags. No
// synthetic records, pricing defaults or legacy provider ports enter assembly.
func newSourceRuntime(cfg config.Config, st *store.Store) (*sourceflow.Service, error) {
	return sourceRuntimeWithHTTP(cfg, st, nil)
}
func sourceRuntimeWithHTTP(cfg config.Config, st *store.Store, h *http.Client) (*sourceflow.Service, error) {
	if st == nil || !sourceOperationsAvailable(sourceRequiredOperations) || !sourceBasesPrepared(cfg) {
		return nil, si.ErrUnconfigured
	}
	clients, e := platform.NewSourceClients(cfg, h)
	if e != nil {
		return nil, si.ErrUnconfigured
	}
	reason := "runtime_output_recovery_unconfigured"
	if cfg.SourceDownloadHosts == "" {
		reason = "source_download_hosts_unconfigured"
	}
	svc := &sourceflow.Service{OutputRecoveryReason: reason, Store: st, Auth: &sourceflow.Authorizer{Store: st, Context: clients.Context, AppID: cfg.AppID}, Bill: clients.Bill, Tasks: clients.Tasks, Assets: sourceRuntimeAssets{AssetPort: clients.Assets, outputConfigured: cfg.SourceDownloadHosts != ""}, Profile: sourceflow.NewProfile(cfg)}
	assemblePlateLock(svc, cfg)
	return svc, nil
}

// assemblePlateLock wires the optional limited-fidelity mode. It never makes the
// process fatal: a missing or rejected sample set only closes the mode, with a
// stable reason that capabilities report. The flag and set are immutable while
// serving.
func assemblePlateLock(svc *sourceflow.Service, cfg config.Config) {
	bi := buildinfo.Read()
	svc.Build = platelock.Build{VCSRevision: bi.VCSRevision, VCSModified: bi.VCSModified}
	svc.PlateEnabled = cfg.BgPlateLockEnabled
	if !cfg.BgPlateLockEnabled {
		return
	}
	if strings.TrimSpace(cfg.FidelitySamplesDir) == "" {
		svc.SamplesReason = "sample_set_unconfigured"
		return
	}
	set, err := fidelitysamples.Load(cfg.FidelitySamplesDir)
	if err != nil {
		svc.SamplesReason = "sample_set_invalid"
		log.Printf("product-image-server: frozen sample set rejected, background_plate_lock stays closed: %v", err)
		return
	}
	svc.Samples = set
}

type sourceRuntimeAssets struct {
	sourceflow.AssetPort
	outputConfigured bool
}

func (a sourceRuntimeAssets) Verify(ctx context.Context, r si.Run) (si.Output, error) {
	if !a.outputConfigured {
		return si.Output{}, si.ErrUnconfigured
	}
	return a.AssetPort.Verify(ctx, r)
}
func (a sourceRuntimeAssets) Download(ctx context.Context, r si.Run) (io.ReadCloser, error) {
	if !a.outputConfigured {
		return nil, si.ErrUnconfigured
	}
	return a.AssetPort.Download(ctx, r)
}
func runSourceRecovery(ctx context.Context, svc *sourceflow.Service) error {
	return svc.RunRecovery(ctx)
}

// Prepare uses the published URL/loopback guards without issuing a request.
// Its literal base-prefix join is preserved; there is no shadow host policy.
func sourceBasesPrepared(cfg config.Config) bool {
	for _, base := range []string{cfg.IdentityBaseURL, cfg.BillingBaseURL, cfg.TaskBaseURL, cfg.UploadBaseURL} {
		u, e := url.Parse(base)
		if e != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
			return false
		}
		if port := u.Port(); port != "" {
			n, e := strconv.Atoi(port)
			if e != nil || n < 1 || n > 65535 {
				return false
			}
		}
		c := pc.Client{Caller: pc.Caller{Mode: "app", AppID: cfg.AppID, Token: cfg.IdentityToken}, Bases: pc.Bases{Identity: base}}
		if _, _, e := c.Prepare(context.Background(), pc.Call{Operation: "identity.context.resolve", RequestID: "source-runtime-config-check"}); e != nil {
			return false
		}
	}
	return true
}

// One owned coordinator; done synchronizes its final error with its joiner.
type sourceCoordinator struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func startSourceCoordinator(ctx context.Context, svc *sourceflow.Service) (*sourceCoordinator, error) {
	if svc == nil {
		return nil, nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	co := &sourceCoordinator{cancel: cancel, done: make(chan struct{})}
	started := make(chan struct{})
	go func() { co.err = svc.RunRecoveryStarted(workerCtx, started); close(co.done) }()
	select {
	case <-started:
		// Immutable for the entire serving lifetime; no request writes this field.
		assets, ok := svc.Assets.(sourceRuntimeAssets)
		svc.OutputRecoveryReady = ok && assets.outputConfigured && svc.Store != nil && svc.Auth != nil && svc.Bill != nil && svc.Tasks != nil
		if svc.OutputRecoveryReady {
			svc.OutputRecoveryReason = ""
		}
		return co, nil
	case <-co.done:
		cancel()
		return co, co.err
	case <-ctx.Done():
		cancel()
		return co, ctx.Err()
	}
}

var errSourceUnjoined = errors.New("source runtime shutdown incomplete: workers or requests not joined")

// Closing the admission gate makes request join deterministic: no Add/Wait race
// and no detached goroutine waiting on handlers that ignore cancellation.
type sourceRequests struct {
	next     http.Handler
	mu       sync.Mutex
	active   int
	stopping bool
	done     chan struct{}
}

func newSourceRequests(next http.Handler) *sourceRequests {
	return &sourceRequests{next: next, done: make(chan struct{})}
}
func (s *sourceRequests) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		http.Error(w, "service stopping", http.StatusServiceUnavailable)
		return
	}
	s.active++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		if s.stopping && s.active == 0 {
			close(s.done)
		}
		s.mu.Unlock()
	}()
	s.next.ServeHTTP(w, r)
}
func (s *sourceRequests) stop() {
	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
		if s.active == 0 {
			close(s.done)
		}
	}
	s.mu.Unlock()
}
func sourceJoin(ctx context.Context, done <-chan struct{}) bool {
	// Prefer an already completed join even when the deadline is also signaled.
	select {
	case <-done:
		return true
	default:
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
func stopSourceProcess(ctx context.Context, cancel context.CancelFunc, srv *http.Server, requests *sourceRequests, co *sourceCoordinator, st *store.Store) error {
	cancel()
	if co != nil {
		co.cancel()
	}
	requests.stop()
	if srv != nil {
		if e := srv.Shutdown(ctx); e != nil {
			_ = srv.Close()
		}
	}
	workerJoined := co == nil || sourceJoin(ctx, co.done)
	requestJoined := sourceJoin(ctx, requests.done)
	if !workerJoined || !requestJoined {
		return errSourceUnjoined
	}
	// Only complete joins grant the main process permission to close its Store.
	return st.Close()
}

var errSourceCoordinatorExited = errors.New("source recovery coordinator exited unexpectedly")

func waitSourceStop(ctx context.Context, served <-chan error, co *sourceCoordinator) error {
	var worker <-chan struct{}
	if co != nil {
		worker = co.done
	}
	select {
	case <-ctx.Done():
		return nil
	case e := <-served:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-worker:
		if ctx.Err() != nil {
			return nil
		}
		return errSourceCoordinatorExited
	}
}

// serveSourceRuntime is the existing main Serve/wait/bounded-stop sequence.
// Tests supply only in-memory listeners; main keeps its original TCP listener
// and explicitly supplies the unchanged ten-second shutdown budget.
func serveSourceRuntime(ctx context.Context, cancel context.CancelFunc, server *http.Server, requests *sourceRequests, coordinator *sourceCoordinator, st *store.Store, ln net.Listener, shutdownBudget time.Duration) (error, error) {
	served := make(chan error, 1)
	go func() { served <- server.Serve(ln) }()
	stopErr := waitSourceStop(ctx, served, coordinator)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownBudget)
	shutdownErr := stopSourceProcess(shutdownCtx, cancel, server, requests, coordinator, st)
	shutdownCancel()
	return stopErr, shutdownErr
}
