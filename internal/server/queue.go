package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Job statuses.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusError   = "error"
)

// Job kinds.
const (
	KindLocus = "locus"
	KindVCF   = "vcf"
)

// Job is one row of the job table (its metadata, without the input/result blobs).
type Job struct {
	ID         string `json:"job_id"`
	Kind       string `json:"kind"`
	Snapshot   string `json:"snapshot"`
	Selection  string `json:"selection"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	NVariants  int64  `json:"n_variants"`
	CreatedAt  int64  `json:"created_at"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
}

// Runner annotates one job's input, returning the result JSON and the number of
// variants. An error marks the job failed (its message is stored on the job).
type Runner func(ctx context.Context, job Job, input []byte) (result []byte, nVariants int, err error)

// Queue is the SQLite-backed async job queue: it persists jobs, their inputs, and
// their results, and drives a worker pool that annotates queued jobs. It uses its
// own database, separate from the annotation cache.
type Queue struct {
	db     *sql.DB
	notify chan struct{}
	nowFn  func() int64

	wg sync.WaitGroup
}

const queueSchema = `
CREATE TABLE IF NOT EXISTS job (
  id          TEXT PRIMARY KEY,
  kind        TEXT    NOT NULL,
  snapshot    TEXT    NOT NULL,
  selection   TEXT    NOT NULL DEFAULT '',
  status      TEXT    NOT NULL,
  error       TEXT,
  n_variants  INTEGER,
  created_at  INTEGER NOT NULL,
  started_at  INTEGER,
  finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS job_status ON job(status, created_at);

CREATE TABLE IF NOT EXISTS job_input  (job_id TEXT PRIMARY KEY, body BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS job_result (job_id TEXT PRIMARY KEY, json TEXT NOT NULL);
`

// OpenQueue opens (creating if needed) the job-queue database at path and prepares
// its schema. Any jobs left "running" from a previous process (a crash) are reset
// to "queued" so the worker pool re-processes them.
func OpenQueue(ctx context.Context, path string) (*Queue, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open server db %s: %w", path, err)
	}
	// Single connection keeps the embedded writer simple and, with WAL + a busy
	// timeout, avoids "database is locked" churn between the workers and HTTP handlers.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	if _, err := db.ExecContext(ctx, queueSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init server db schema: %w", err)
	}
	// Crash recovery: requeue anything left mid-flight.
	if _, err := db.ExecContext(ctx,
		`UPDATE job SET status=?, started_at=NULL WHERE status=?`, StatusQueued, StatusRunning); err != nil {
		db.Close()
		return nil, fmt.Errorf("requeue running jobs: %w", err)
	}
	return &Queue{db: db, notify: make(chan struct{}, 1), nowFn: func() int64 { return time.Now().Unix() }}, nil
}

// Close closes the queue database.
func (q *Queue) Close() error { return q.db.Close() }

// newID returns a random 128-bit hex job id.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Enqueue records a new queued job (metadata + input body) and wakes a worker.
func (q *Queue) Enqueue(ctx context.Context, kind, snapshot, selection string, body []byte) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job(id,kind,snapshot,selection,status,created_at) VALUES(?,?,?,?,?,?)`,
		id, kind, snapshot, selection, StatusQueued, q.nowFn()); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job_input(job_id,body) VALUES(?,?)`, id, body); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	q.poke()
	return id, nil
}

// poke wakes one waiting worker (non-blocking).
func (q *Queue) poke() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Get returns a job's metadata (ok=false when the id is unknown).
func (q *Queue) Get(ctx context.Context, id string) (Job, bool, error) {
	row := q.db.QueryRowContext(ctx,
		`SELECT id,kind,snapshot,selection,status,error,n_variants,created_at,started_at,finished_at
		 FROM job WHERE id=?`, id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

// Result returns a done job's result JSON (ok=false when the id is unknown or the
// job has no stored result yet).
func (q *Queue) Result(ctx context.Context, id string) ([]byte, bool, error) {
	var js string
	err := q.db.QueryRowContext(ctx, `SELECT json FROM job_result WHERE job_id=?`, id).Scan(&js)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return []byte(js), true, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanJob(row rowScanner) (Job, error) {
	var j Job
	var errStr sql.NullString
	var nVar, started, finished sql.NullInt64
	if err := row.Scan(&j.ID, &j.Kind, &j.Snapshot, &j.Selection, &j.Status,
		&errStr, &nVar, &j.CreatedAt, &started, &finished); err != nil {
		return Job{}, err
	}
	j.Error = errStr.String
	j.NVariants = nVar.Int64
	j.StartedAt = started.Int64
	j.FinishedAt = finished.Int64
	return j, nil
}

// StartWorkers launches n worker goroutines that claim and process queued jobs
// until ctx is cancelled. Call Wait to block for their shutdown.
func (q *Queue) StartWorkers(ctx context.Context, n int, runner Runner) {
	if n < 1 {
		n = 1
	}
	for i := 0; i < n; i++ {
		q.wg.Add(1)
		go q.worker(ctx, runner)
	}
}

// Wait blocks until all workers have stopped (after ctx cancellation).
func (q *Queue) Wait() { q.wg.Wait() }

func (q *Queue) worker(ctx context.Context, runner Runner) {
	defer q.wg.Done()
	ticker := time.NewTicker(time.Second) // fallback poll (also covers missed pokes)
	defer ticker.Stop()
	for {
		// Drain all currently-queued jobs before sleeping.
		for {
			if ctx.Err() != nil {
				return
			}
			job, input, ok, err := q.claimNext(ctx)
			if err != nil {
				log.Printf("cganno server: claim job: %v", err)
				break
			}
			if !ok {
				break
			}
			q.process(ctx, job, input, runner)
		}
		select {
		case <-ctx.Done():
			return
		case <-q.notify:
		case <-ticker.C:
		}
	}
}

// claimNext atomically claims the oldest queued job, marking it running. ok=false
// when there is nothing queued.
func (q *Queue) claimNext(ctx context.Context) (Job, []byte, bool, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, nil, false, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx,
		`SELECT id,kind,snapshot,selection,status,error,n_variants,created_at,started_at,finished_at
		 FROM job WHERE status=? ORDER BY created_at, id LIMIT 1`, StatusQueued)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return Job{}, nil, false, nil
	}
	if err != nil {
		return Job{}, nil, false, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE job SET status=?, started_at=? WHERE id=? AND status=?`,
		StatusRunning, q.nowFn(), job.ID, StatusQueued)
	if err != nil {
		return Job{}, nil, false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Job{}, nil, false, nil // lost the claim to another worker
	}
	var body []byte
	if err := tx.QueryRowContext(ctx, `SELECT body FROM job_input WHERE job_id=?`, job.ID).Scan(&body); err != nil {
		return Job{}, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return Job{}, nil, false, err
	}
	job.Status = StatusRunning
	return job, body, true, nil
}

// process runs the job's runner and records its outcome.
func (q *Queue) process(ctx context.Context, job Job, input []byte, runner Runner) {
	result, nVar, err := runner(ctx, job, input)
	if err != nil {
		if _, uerr := q.db.ExecContext(ctx,
			`UPDATE job SET status=?, error=?, finished_at=? WHERE id=?`,
			StatusError, err.Error(), q.nowFn(), job.ID); uerr != nil {
			log.Printf("cganno server: mark job %s errored: %v", job.ID, uerr)
		}
		return
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("cganno server: store job %s result: %v", job.ID, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job_result(job_id,json) VALUES(?,?)
		 ON CONFLICT(job_id) DO UPDATE SET json=excluded.json`, job.ID, string(result)); err != nil {
		log.Printf("cganno server: store job %s result: %v", job.ID, err)
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE job SET status=?, n_variants=?, finished_at=? WHERE id=?`,
		StatusDone, nVar, q.nowFn(), job.ID); err != nil {
		log.Printf("cganno server: finish job %s: %v", job.ID, err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("cganno server: commit job %s: %v", job.ID, err)
	}
}
