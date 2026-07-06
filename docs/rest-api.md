# REST API

`cganno server` runs an asynchronous annotation service over HTTP: it exposes the same
annotation engine as the CLI, backed by the same snapshot config and cache. Annotation can be
expensive (external tools, large lookups), so requests are **queued** — a submit returns a
**job id** immediately; a worker pool annotates in the background; the caller **polls** by job
id and fetches JSON results when the job is done. Jobs and results persist in a dedicated
SQLite database.

## Configuration

Add a `[server]` block to `config.toml` (see [`config.example.toml`](../config.example.toml)):

```toml
[server]
endpoint   = "127.0.0.1:8080"                 # IP:port to listen on
master_key = "change-me-to-a-secret"          # HMAC key signing /v1 API tokens
workers    = 1                                 # async worker pool size (default 1)
db         = "$CGANNO_HOME/cganno_server.db"   # job-queue + results DB (default ./cganno_server.db)
```

Run it:

```sh
cganno server                 # uses the [server] endpoint
cganno server -addr :9000     # override the endpoint
```

The server uses the active snapshot (config `default_snapshot`, or the global `-snapshot`
flag) and the annotation cache from `[database]`. It runs the **full locus path**, including
`type="tool"` sources (VEP/ANNOVAR) — same as `cganno annotate <locus>`.

## Authentication

The `/v1/*` API is authenticated with an HMAC-signed bearer token. **On startup the server
prints one valid token to stdout** (logs go to stderr):

```sh
$ cganno server
eyJzdWIiOiJjZ2Fubm8i….fJfpi6WBBCUX-5G4FrXnYh78meDvTtjMJIy3sb0pO6E   # <- the token (stdout)
2026/07/05 01:28:38 cganno server: snapshot "2026-07", 2 worker(s); listening on http://127.0.0.1:8080
```

Send it as `Authorization: Bearer <token>` on every `/v1` request. A token is
`b64url(payload).b64url(HMAC-SHA256(master_key, b64url(payload)))`; any token correctly signed
by `master_key` is accepted (tokens do not expire in this version). The browser form and its
`/ui/*` endpoints are **open** (no token) for local/trusted-network convenience.

## Endpoints

### Authenticated API (`/v1`, bearer token)

| method + path | body | response |
| --- | --- | --- |
| `GET /v1/annotations` | — | the snapshot's sources + annotation fields, with the default set marked (for discovery/selection) |
| `POST /v1/annotate` | `{ "locus": "chrom:pos:ref:alt", "annotations": "all"｜["a",…] }` | `202 { "job_id": "…" }` |
| `POST /v1/annotate/vcf` | `multipart/form-data`: file field `vcf`, optional `annotations` form field | `202 { "job_id": "…" }` |
| `GET /v1/jobs/{id}` | — | job status object |
| `GET /v1/jobs/{id}/results` | — | the results array (see below), or `409` if not finished |

`annotations` is optional — omit it for the snapshot's **default** annotations, pass `"all"`
for every annotation, or a **list of names** (from `GET /v1/annotations`) to select specific
ones. Unknown names / malformed loci are rejected at submit time with `400`.

`GET /v1/jobs/{id}` returns:

```json
{ "job_id": "…", "kind": "locus", "snapshot": "2026-07", "selection": "",
  "status": "queued|running|done|error", "error": "", "n_variants": 1,
  "created_at": 1783229365, "started_at": 1783229365, "finished_at": 1783229365 }
```

### Results

Results honor the same JSON shape as the CLI's [`--format json`](io-formats.md#annotate-output):
an array of per-variant objects, in input order, each carrying its coordinates and a
name→value map (every selected annotation is a key, `null` when the locus has no match):

```json
[ { "chrom": "chr2", "pos": 200, "ref": "C", "alt": "T",
    "annotations": { "auto_id": "chr2_200_C_T", "tstv": "TS" } } ]
```

For a **VCF upload** there is one object **per line, in file order** — a multi-allelic ALT is
split into one object per allele, each carrying its own `chrom/pos/ref/alt`.

### Browser form (open, no token)

- `GET /` — a minimal HTML form: enter a `chrom:pos:ref:alt` locus, tick the annotations to
  return (fetched from `/ui/annotations`, defaults pre-checked), submit. Its JavaScript posts
  the job, polls its status, and renders the result as a tall table.
- `GET /ui/annotations`, `POST /ui/submit`, `GET /ui/jobs/{id}`, `GET /ui/jobs/{id}/results` —
  unauthenticated twins of the `/v1` endpoints that the form's JS uses.

## Example

```sh
TOKEN=$(cganno server | head -1)          # capture the startup token (run in the background)
BASE=http://127.0.0.1:8080

# discover available annotations
curl -s -H "Authorization: Bearer $TOKEN" $BASE/v1/annotations

# submit a locus, get a job id, poll, fetch results
JID=$(curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -d '{"locus":"chr1:115256529:T:C"}' $BASE/v1/annotate | jq -r .job_id)
curl -s -H "Authorization: Bearer $TOKEN" $BASE/v1/jobs/$JID            # {"status":"done",…}
curl -s -H "Authorization: Bearer $TOKEN" $BASE/v1/jobs/$JID/results    # [ {…} ]

# annotate a whole VCF (all annotations)
curl -s -H "Authorization: Bearer $TOKEN" \
     -F vcf=@variants.vcf -F annotations=all $BASE/v1/annotate/vcf
```

← Back to the **[docs index](README.md)**.
