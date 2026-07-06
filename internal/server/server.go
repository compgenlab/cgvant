// Package server implements the `cganno server` async REST annotation service.
// It exposes the same annotation engine as the CLI over HTTP: a request submits a
// locus (or an uploaded VCF) and is queued; a worker pool annotates it; the client
// polls by job id and fetches JSON results. The /v1 API is authenticated with
// HMAC bearer tokens (see auth.go); a browser form and its /ui/* endpoints are
// open. Jobs and results persist in a dedicated SQLite database (see queue.go).
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/compgenlab/cganno/internal/config"
	"github.com/compgenlab/cganno/internal/engine"
	"github.com/compgenlab/cganno/internal/model"
	"github.com/compgenlab/cganno/internal/service"
	"github.com/compgenlab/cganno/internal/store"
	"github.com/compgenlab/cganno/internal/vcf"
)

// Server holds the running annotation service: the loaded config + snapshot, the
// shared annotation-cache store, and the job queue.
type Server struct {
	cfg   *config.Config
	snap  *config.Snapshot
	store store.Store // annotation cache (may be nil when the cache is disabled)
	queue *Queue
}

// New builds a Server over an already-loaded config/snapshot, the (optional)
// annotation-cache store, and an open job queue.
func New(cfg *config.Config, snap *config.Snapshot, st store.Store, q *Queue) *Server {
	return &Server{cfg: cfg, snap: snap, store: st, queue: q}
}

// Run starts the worker pool, prints a valid API token to stdout, and serves HTTP
// on the configured endpoint until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	s.queue.StartWorkers(ctx, s.cfg.Server.Workers, s.runJob)

	token, err := MintToken(s.cfg.Server.MasterKey, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("mint startup token: %w", err)
	}
	// The startup token goes to STDOUT (so it can be captured); logs go to STDERR.
	fmt.Fprintln(os.Stdout, token)
	log.Printf("cganno server: snapshot %q, %d worker(s); listening on http://%s",
		s.snap.Name, s.cfg.Server.Workers, s.cfg.Server.Endpoint)

	httpSrv := &http.Server{Addr: s.cfg.Server.Endpoint, Handler: s.routes()}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		s.queue.Wait()
		return nil
	}
}

// routes builds the HTTP mux: an authenticated /v1 API and an open browser UI.
func (s *Server) routes() http.Handler {
	// Authenticated API surface.
	api := http.NewServeMux()
	api.HandleFunc("GET /v1/annotations", s.handleAnnotations)
	api.HandleFunc("POST /v1/annotate", s.handleAnnotateLocus)
	api.HandleFunc("POST /v1/annotate/vcf", s.handleAnnotateVCF)
	api.HandleFunc("GET /v1/jobs/{id}", s.handleJob)
	api.HandleFunc("GET /v1/jobs/{id}/results", s.handleJobResults)

	mux := http.NewServeMux()
	mux.Handle("/v1/", requireToken(s.cfg.Server.MasterKey, api))

	// Open browser UI (no token): the form page + the endpoints its JS calls.
	mux.HandleFunc("GET /{$}", s.handleForm)
	mux.HandleFunc("GET /ui/annotations", s.handleAnnotations)
	mux.HandleFunc("POST /ui/submit", s.handleAnnotateLocus)
	mux.HandleFunc("GET /ui/jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /ui/jobs/{id}/results", s.handleJobResults)
	return mux
}

// runJob is the queue Runner: it parses the job's input into loci, resolves the
// requested annotation selection, annotates over the shared locus path, and
// returns the JSON result array (engine.BuildVariants — identical to the CLI's
// --format json). The number of variants is the locus count (per ALT allele).
func (s *Server) runJob(ctx context.Context, job Job, input []byte) ([]byte, int, error) {
	loci, err := lociFromInput(job.Kind, input)
	if err != nil {
		return nil, 0, err
	}
	selected, err := resolveSelection(s.snap, job.Selection)
	if err != nil {
		return nil, 0, err
	}
	names := annotationNames(selected)

	// A VCF upload is a bulk request: tools run over the whole input directly
	// (per-locus cache skipped). A single-locus job keeps the cache.
	skipToolCache := job.Kind == KindVCF
	res, err := service.AnnotateLoci(ctx, s.cfg, s.snap, s.store, selected, loci, skipToolCache)
	if err != nil {
		return nil, 0, err
	}
	out, err := json.Marshal(engine.BuildVariants(loci, names, res))
	if err != nil {
		return nil, 0, err
	}
	return out, len(loci), nil
}

// lociFromInput turns a job's stored input body into loci: a single locus string
// for a locus job, or a parsed VCF for a vcf job (multi-allelic ALTs split, in
// file order).
func lociFromInput(kind string, input []byte) ([]model.Locus, error) {
	switch kind {
	case KindLocus:
		l, err := vcf.ParseLocus(string(input))
		if err != nil {
			return nil, err
		}
		return []model.Locus{l}, nil
	case KindVCF:
		return vcf.Read(bytes.NewReader(input))
	default:
		return nil, fmt.Errorf("unknown job kind %q", kind)
	}
}

// --- small HTTP helpers ----------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
