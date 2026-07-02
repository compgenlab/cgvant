# cgtag

**Fast, no-fuss variant annotation from the command line.**

`cgtag` annotates VCF files (and bare loci) against a *versioned* bundle of
reference sources — gene model, ClinVar significance, gnomAD allele frequencies,
CADD/REVEL scores, your own BED/VCF/TSV tracks — and caches the results so repeat
work is instant. It's a single static binary: no Perl, no cache-install dance, no
database server to stand up.

> The name: `cg` for [compgenlab](https://github.com/compgenlab), and the tool
> writes its self-contained annotations as `CG_*` INFO tags.

## Why cgtag

[VEP](https://www.ensembl.org/info/docs/tools/vep/) and
[ANNOVAR](https://annovar.openbioinformatics.org/) are powerful but notoriously
fiddly to install and run; [vcfanno](https://github.com/brentp/vcfanno) is lean
but leaves versioning, downloading, and caching to you. cgtag aims for the middle:

- **One binary.** Pure Go (`CGO_ENABLED=0`), cross-compiles to Linux/macOS,
  amd64/arm64. No interpreter or system libraries to manage.
- **Versioned bundles.** A **snapshot** pins a set of `name:version` sources +
  annotation schema — a directory with one small TOML file per source, so it's
  git-friendly and easy to edit. The snapshot name *is* the version stamped on
  every result, so output is reproducible.
- **Memoizing cache.** Each locus is annotated once and served from SQLite
  thereafter — so expensive lookups don't repeat across runs or files.
- **Config-driven.** Sources and the fields they expose are declared in TOML; no
  code to add a new annotation.
- **Locus-keyed & sample-blind.** Annotation depends only on
  `chrom/pos/ref/alt`; `GT`/`FORMAT` are never required for reference annotation.

## Status

Early but working: interactive CLI on a SQLite backend. A Postgres backend and a
REST annotate endpoint are planned (see [Roadmap](#roadmap)). Cohort-style
filtering ("which loci are pathogenic AND rare") is intentionally **out of scope**
— cgtag produces the annotations; a consumer filters them.

## Install

Requires Go 1.25+.

```sh
go install github.com/compgenlab/cgtag/cmd/cgtag@latest
```

Or build from source:

```sh
git clone https://github.com/compgenlab/cgtag
cd cgtag
make build            # -> bin/cgtag
```

cgtag is pure Go (`CGO_ENABLED=0`) — including its annotation engine, the
[`hts`](https://github.com/compgenlab/hts) library, and the SQLite driver
([modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)) — so it needs no C
toolchain and cross-compiles cleanly. `make help` lists all targets (`test`,
`vet`, `cross`, `install`, …); `make cross` builds release tarballs for
linux/darwin × amd64/arm64.

## Quick start

```sh
export CGTAG_HOME=~/cgtag          # base dir for config, data, cache, and the DB
cgtag init                         # interactive: prompts for the annotations dir,
                                   #   scaffolds config.toml + sources/ + a snapshot
                                   #   manifest, then offers to open the configure TUI

# add a reusable source and reference it from the snapshot, then fetch the data:
cgtag source add --name gnomad --version 4.1 --url https://… --format vcf --snapshot 2026-06
cgtag annotation add --source gnomad:4.1 --name gnomad_af --field AF --type numeric
cgtag download -j 4                # fetch + tabix-index the snapshot's sources (4 at once)

# annotate (no flag = the snapshot's default_annotations):
cgtag annotate chr1:115256529:T:C        # a locus -> TSV of its annotations (cached after first)
cgtag annotate --all --format vcf -o out.vcf in.vcf  # a VCF -> a fully-annotated VCF (all annotations)
cgtag annotate -a clinvar_sig,af in.vcf  # only the named annotations
```

`cgtag annotate` writes **TSV by default** (`--format tab`) and also supports
`--format vcf|json|text`; `-o FILE` writes the output to a file (default stdout) for any
format. Which annotations are applied is chosen by `--all`, `-a name[,name…]`, or (with no
flag) the snapshot's `default_annotations`. Set the defaults in the snapshot manifest
(or via the TUI); `cgtag annotation list <snapshot>` shows the default set (`*`).

`CGTAG_HOME` is the base directory holding `config.toml`; it's chosen as the
`-home DIR` flag, else `$CGTAG_HOME`, else the current directory. Config values may
reference it as `$CGTAG_HOME` (e.g. `data_dir = "$CGTAG_HOME/data"`), so data,
cache, snapshots, and the database all live under the home no matter where you run
cgtag from.

## Concepts

| Concept | What it is |
| --- | --- |
| **Snapshot** | A manifest file `snapshots/<name>.toml` that references reusable sources by `name:version` and carries snapshot-scoped settings (`assembly`, `default_annotations`). Its reference FASTA is looked up from the config by `assembly`. Apply order = list order. The snapshot name is the version stamped on results. |
| **Source** | The one core unit, discriminated by `type`: a data file (default — `vcf`/`bed`/`tab`/`gtf`), `type = "builtin"`, or `type = "tool"`. Identified `name:version` (a docker-style tag), living at `sources/<name>/<version>/`. Reusable: one source can be referenced by many snapshots. Its annotations nest under it (`[[sources.annotations]]`). A `type = "tool"` source is an external, often containerized per-query annotator (VEP/ANNOVAR) whose generated output is consumed like a data source (see below). |
| **Annotation** | A declared output field nested under its source: a `name` (the new INFO tag added) filled from the source's `field` (an INFO id / BED or TSV column / `@ID`). `field` defaults to `name`. |
| **Builtin** | A self-contained "built-in tool call" (no data file), declared in a `[[sources]]` of `type = "builtin"` whose nested annotations name the builtin (`builtin = "tstv"`); emitted as `CG_*` tags, applied in `annotate -o`. *Variant-only* (`auto_id`, `indel`, `tstv`, `vardist`, `tags`) work on any VCF; *sample-derived* (`dosage`, `vaf`, `minor_strand`, `fisher_sb`, `copy_logratio`) read per-sample `FORMAT` (GT/SAC/AD) and need a VCF with samples. Parameterized ones (`tags`, `copy_logratio`) take an `args` value. |
| **Cache** | An **optional** SQLite EAV table memoizing computed annotations, keyed by assembly + locus + source `name:version`. Omit `[database]` to skip it entirely (nothing is written); add it to memoize. Shared safely across snapshots; GRCh37/GRCh38 never collide. |

### Source formats

- **`vcf`** — copy an INFO field (`field = "CLNSIG"`), the record ID
  (`field = "@ID"`), or presence (`type = "flag"`). `match = "exact"` (REF+ALT,
  default) or `"position"`; `unique = true` de-duplicates multiple matches.
- **`bed`** — interval overlap; `field = "name"` (col 4), `"score"` (col 5), or a
  column number.
- **`tab`** — a tabix-indexed TSV (REVEL, CADD, …). `ref_col`/`alt_col` (1-based)
  are **optional**: set them for allele-aware matching, omit them for position-only.
  The annotation's `field` is the value column (number or header name),
  `type = "numeric"` for scores.

### Annotation types

`type` controls how a value is interpreted and stored (default `categorical`):

- **`categorical`** — a string from a limited set (e.g. `CLNSIG`); an optional
  `values = [...]` documents the enum.
- **`text`** — a free-form string (descriptions, HGVS, gene lists). Same storage as
  categorical; the distinction is schema intent (not a small value set).
- **`numeric`** — a float; stored numerically (`value_num`) so comparisons stay
  indexable. A value that doesn't parse as a number is dropped for that locus.
- **`flag`** — presence only, no value; `field` is ignored.

### Assembly, indexes & integrity

Each source may declare:

- **`assembly`** (e.g. `GRCh38`) — verified against the **including snapshot's**
  `assembly` at load time, so a snapshot can't silently mix a GRCh37 source into a
  GRCh38 build (mismatch is a hard error). Assembly is per-snapshot (a snapshot is
  inherently assembly-specific) — there is no global assembly. `cgtag source add
  --snapshot S` auto-tags new sources with `S`'s assembly.
- **`url` vs `localpath`** — `url`/`url_index` are the canonical reference (kept for
  provenance and the registry). `localpath`/`localpath_index` are this machine's
  actual files: when `localpath` is set the file is used **exactly and never
  downloaded** (the index is expected alongside, or at `localpath_index`). A relative
  `localpath` resolves under `data_dir`, and environment variables are expanded
  (`${CACHE_DIR}/…`, `$CGTAG_HOME/…`). `registry submit` strips localpath fields.
- **`url_index`** — a prebuilt `.tbi`/`.csi` to download directly. Otherwise
  `download` guesses one alongside `url`, and only builds locally as a last resort.
- **Multi-file sources** — one source, several files, all fetched by `download` and
  queried together at annotation time:
  - `chroms` + a `{chrom}` template in `url`/`localpath` — many files sharded by chromosome
    (e.g. gnomAD per chromosome).
  - `[[sources.files]]` — an explicit list (each with its own `url`/`checksum`) for a
    small union split by something else (e.g. coding + indels — same format, different
    ref/alt); every file is queried and ref/alt picks the match.
- **Built sources** (`[[sources.build]]`) — for data that needs preprocessing before
  tabix can use it (e.g. REVEL: many CSV zips → convert → merge → index). The recipe
  lives in the fragment, so the source stays self-contained and registry-shareable:
  ```toml
  [[sources]]
  name = "revel"
  version = "1.3"
  format  = "tab"
  ref_col = 3
  alt_col = 4
  url     = "https://sites.google.com/site/revelgenomics/"   # provenance
  requires = ["unzip", "python3"]                           # host deps preflighted
    [sources.build]
    output = "merged.revel.hg38.txt.gz"
    inputs = ["https://.../revel_chrom_21_*.csv.zip", "..."]   # downloaded into {inputs}/
    assets = ["convert_csv_to_tab.py"]                         # URL, or co-located file
    run = [                                                    # must write {output}
      "for z in {inputs}/*.zip; do unzip -o $z -d {workdir}/segments; done",
      "python3 {workdir}/convert_csv_to_tab.py {workdir}/segments/*.csv | cgtag bgzip -o {output} -s 1 -b 2 -e 2 -S 1",
    ]
    [[sources.annotations]]
    name = "revel"
    field = "7"
    type = "numeric"
  ```
  `cgtag download` runs the recipe once (cached; `--force` rebuilds). Step placeholders:
  `{workdir} {inputs} {output} {threads}`. **Assets** are URLs or paths relative to the
  fragment dir — a relative asset ships next to the source in the registry, and `registry
  add-source` pulls both (see `registry-repo-scaffold/sources/revel/1.3/revel-1.3.toml`).
- **Built-in `bgzip`/`tabix`** — recipes and tool scripts can call `cgtag bgzip` and
  `cgtag tabix` instead of the external programs (backed by the `hts` library), so
  those aren't required deps. `cgtag bgzip [-o FILE] [file]` BGZF-compresses a file or
  stdin; adding a tabix preset/columns (`-p vcf|bed|gff`, or `-s`/`-b`/`-e`/`-S`/`-c`/`-0`)
  **with `-o`** also writes the index in one step. `cgtag tabix [preset|cols] FILE`
  indexes an existing `.gz`. (Needs `cgtag` on `PATH` — it is when you run cgtag.)
- **Required software** (`requires`) — a tool or build source can list the host
  executables it needs (`requires = ["python3", "bgzip"]`). `cgtag download` and
  `cgtag annotate` check them with one `LookPath` up front and fail fast with a clear
  message if any is missing, instead of erroring partway through a run. A tool's
  container engine (apptainer/singularity) is checked automatically, so it needn't be
  listed.
- **organism** — `assembly` is just a string, so any organism works
  (`assembly = "GRCm39"`, etc.); it's only checked for consistency, never assumed.
- **`checksum`** / **`checksum_index`** — `"<algo>:<hex-or-url>"` (`md5`, `sha1`,
  `sha256`), **optional**; verified while downloading when present (a mismatch fails and
  leaves no partial file). The value may be a **URL** to a checksum file (md5sum-style
  manifest or single hash), `{chrom}`-templated for multi-file sources.

## Configuration

A small global `config.toml`, plus an **`annotations_dir`** tree holding reusable
sources and the snapshot manifests that reference them. The versioned dirs
mirror the registry 1:1, so multiple versions coexist and local↔registry paths match:

```
<annotations_dir>/
  sources/<name>/<version>/<name>-<version>.toml   (+ co-located build/helper assets)
  snapshots/<name>.toml                            (manifest: refs + settings)
```

Tool sources (`type = "tool"`) live under `sources/` like any other source; only
their downloaded images/data are cached separately (under `cache/tools/<name>/<version>/`).

See `config.example.toml` and the `examples/` directory for a full worked tree.
Upgrading an older install? See [MIGRATION.md](MIGRATION.md).

```toml
# config.toml — lives in CGTAG_HOME; values may reference $CGTAG_HOME
data_dir         = "$CGTAG_HOME/data"        # base for relative `localpath` files
cache_dir        = "$CGTAG_HOME/data/cache"  # downloaded sources + tool images/data,
                                             #   keyed name/version (tool images/data under cache/tools/<name>/<version>/)
default_snapshot = "2026-06"
annotations_dir  = "./annotations"           # root for sources/, snapshots/ (default: $CGTAG_HOME/annotations)

[database]                                   # OPTIONAL — omit to skip the cache entirely
backend = "sqlite"                           # postgres planned
path    = "$CGTAG_HOME/cgtag.db"             # annotation cache, incl. per-locus tool output

[references.GRCh38]                          # reference FASTAs keyed by assembly;
fasta = "/data/ref/GRCh38.fa"                #   a snapshot's is looked up by its assembly
                                             #   (for tools + normalization; not for overlay)
```

A snapshot manifest references its sources by `name:version` and holds the
snapshot-scoped settings — including which annotations apply by default:

```toml
# snapshots/2026-06.toml
description         = "GRCh38 clinical annotation set"
assembly            = "GRCh38"                       # scopes the cache + verifies sources;
                                                     #   its reference FASTA comes from config
sources             = ["builtins:1", "clinvar:2026-01", "gnomad:4.1", "vep:112"]  # apply order = list order; vep:112 is a type="tool" source
default_annotations = ["clinvar_sig", "gnomad_af", "vep_consequence"]
```

Each source is a reusable fragment at `sources/<name>/<version>/`; its annotations
nest under it (no `source =`). Defaults are *not* set here — they live in the
snapshot manifest, so one source can be default in one snapshot and opt-in in another:

```toml
# sources/clinvar/2026-01/clinvar-2026-01.toml
[[sources]]
name = "clinvar"
version = "2026-01"
format = "vcf"
url = "https://ftp.ncbi.nlm.nih.gov/pub/clinvar/vcf_GRCh38/clinvar.vcf.gz"
  [[sources.annotations]]
  name = "clinvar_sig"
  field = "CLNSIG"
  type = "categorical"
```

Builtins live in a `type = "builtin"` source; each annotation names its builtin:

```toml
# sources/builtins/1/builtins-1.toml
[[sources]]
name = "builtins"
version = "1"
type = "builtin"
  [[sources.annotations]]
  builtin = "tstv"
  [[sources.annotations]]
  builtin = "tags"
  args = "PANEL:v1"
```

An external annotator is a `type = "tool"` source at `sources/<name>/<version>/`
with nested `[[sources.annotations]]`. `cgtag download` pulls its `image` and runs
`[[sources.setup]]` once; on `annotate` the `[[sources.steps]]` run over the input and the
output is consumed as a source, using the snapshot's `reference` FASTA. The output is
**cached per locus** (keyed `name:version` + the snapshot's assembly), so the tool runs
only on loci it hasn't seen — bump `version` on any image/setup/steps change to
invalidate it:

```toml
# sources/vep/112/vep-112.toml
[[sources]]
name     = "vep"
version  = "112"
type     = "tool"
image    = "docker://ensemblorg/ensembl-vep"  # pulled by `cgtag download` (or a downloadable .sif URL)
engine   = "apptainer"
format   = "vcf"                              # VEP output is a VCF; pull INFO ids from it
output   = "vep.vcf.gz"
requires = ["python3", "bgzip"]               # host deps for the post-process step (apptainer auto-checked)
  [[sources.setup]]                           # one-time, runs INSTALL.pl into {datadir}
  container = true
  run = "INSTALL.pl -c {datadir} -r {datadir}/plugins -a cfp -g CSN -s homo_sapiens -y GRCh38"
  [[sources.steps]]                           # 1) VEP inside the image → intermediate VCF in {workdir}
  container = true
  run = "vep -i {input} -o {workdir}/vep.vcf --vcf --everything --offline --cache --dir_cache {datadir} --dir_plugins {datadir}/plugins --fasta {ref} --fork {threads}"
  [[sources.steps]]                           # 2) post-process on the host: expand/rename INFO, pick worst
  container = false
  run = "expand_vep_vcf.py < {workdir}/vep.vcf | vep_vcf_worst_consequence.py | bgzip > {output}"
  [[sources.annotations]]                     # one per INFO id to pull; field = the INFO tag VEP emits
  name  = "vep_consequence"
  field = "Consequence"
  type  = "categorical"
```

**Container steps run with fixed mountpoints.** Inside a `container = true` step the
placeholders resolve to stable in-container paths — *independent of the end user's
host layout* — and cgtag binds the matching host dirs to them:

| placeholder | in-container value | host dir bound there |
|---|---|---|
| `{datadir}` | `/cgtag/data` | the tool's persistent data dir |
| `{workdir}` | `/cgtag/work` | the per-run scratch dir |
| `{ref}` | `/cgtag/ref/<file>` | the reference FASTA's dir |
| `{input}` | `/cgtag/in/<file>` | the input VCF's dir |
| `{output}` | `/cgtag/work/<file>` | (written under the workdir) |

So `INSTALL.pl -c {datadir}` runs as `INSTALL.pl -c /cgtag/data` on every machine,
which is why a tool source is portable enough to share via a registry — the
author never has to know where the end user's cache lives. (The engine only creates
shallow `/cgtag/*` mountpoints; it never has to recreate a deep host path inside the
read-only image.)

A step with `container = false` runs on the **host**, not inside the image, and its
placeholders keep **real host paths** — handy for post-processing scripts (here
`expand_vep_vcf.py` / `vep_vcf_worst_consequence.py`; that host `python3` is why it's
in `requires`). The workdir is shared between container and host steps (host workdir ⇄
`/cgtag/work`), so the container VEP step writes `{workdir}/vep.vcf` and the host
post-process reads the same file.

**Helper scripts → `assets`.** A tool source lists any co-located helper files it needs in
`assets = ["expand_vep_vcf.py", …]` (filenames next to the source's `.toml`, or URLs).
cgtag stages each into the step workdir before every run, so a step references it
explicitly as `{workdir}/expand_vep_vcf.py` — no `PATH` or shebang reliance, and it
works in host *and* container steps (the workdir is bound at `/cgtag/work`). Declaring
them also lets the registry bundle the scripts with the tool source.

**`input_format`** controls how the query variants are written to `{input}`. The
default `"vcf"` materializes a VCF (samples preserved for a VCF input; a sites-only VCF
is synthesized for bare loci) — right for VEP and other VCF-in tools. Set a per-variant
line template to feed a variant-list tool like ANNOVAR, e.g.
`input_format = "{chrom}\t{pos}\t{ref}\t{alt}"`, with placeholders `{chrom}` `{pos}`
`{pos0}` `{ref}` `{alt}` `{end}`. (`format` — vcf|tab — is separate: it's how the tool's
*output* is read.)

See `examples/` for a runnable tree covering every source shape.

### Editing (TUI)

`cgtag edit [snapshot]` (alias `cgtag configure`) opens an interactive terminal
editor: browse snapshots → their sources/builtins (each with a ✓/⚠
completeness badge) → forms for every field and nested annotation. It both creates
and edits — add a snapshot, a source, a builtin, or an annotation from an empty form
that shows the required/missing fields, and saves each into its version dir. On a
snapshot, two checkbox editors manage the manifest: **members** (`m`) toggles which
available sources the snapshot includes, and **defaults** (`d`) picks the
`default_annotations` from the annotations those sources provide. For scripting,
the flag commands (`source add`, `annotation add`, `snapshot add --default …`) do the
same writes non-interactively.

### Registry

A **registry** is a catalog of pre-made source/snapshot *configs* (not data), served
as a plain static **`registry.toml`** over HTTPS — GitHub raw, Pages, S3, any web
host. Its layout mirrors the local tree exactly: `sources/<name>/<version>/<name>-<version>.toml`
plus `snapshots/<name>.toml` manifests. When none is
configured, cgtag uses the built-in default
([`cgtag-public-data-registry`](https://github.com/compgenlab/cgtag-public-data-registry)).
Configure your own (one or several) in `config.toml`:

```toml
registries = [
  "https://raw.githubusercontent.com/compgenlab/cgtag-public-data-registry/main/registry.toml",
  "https://example.org/my-lab-registry/registry.toml",   # searched in order
]
```

```sh
cgtag registry list                                 # catalog snapshots + sources (all registries)
cgtag registry pull-snapshot <snapshot-name>        # write snapshots/<name>.toml + its sources
cgtag snapshot add <snapshot-name>                  # (or create an empty snapshot first)
cgtag registry add-source <snapshot-name> clinvar   # download a source fragment into the snapshot
cgtag registry add-tool   <snapshot-name> vep       # alias for add-source (a tool is a type="tool" source)
```

`add-source`/`add-tool` fetch the fragment **and** its co-located helper assets
(build scripts / tool post-processing scripts) into the snapshot. Versions are tags:
`vep:113` pins one, bare `vep` / `vep:latest` take the entry marked `latest`.

Versions are **tags** (docker-style): `add-source clinvar:2026-01` pins a version,
while bare `clinvar` or `clinvar:latest` resolve to the entry the registry marks
`latest = true`. cgtag doesn't auto-sort versions (semver `1.3`, dbSNP `b157`, dates
aren't comparable), so the publisher declares which is latest.

**Contributing a source or tool.** Build and test it in a local snapshot, then submit:

```sh
cgtag registry submit <snapshot-name> clinvar --dry-run   # preview the issue (no token needed)
GITHUB_TOKEN=<classic public_repo PAT> \
  cgtag registry submit <snapshot-name> vep         # a type="tool" source works too (name[:version])
```

`submit` reads the named source fragment (a `type="tool"` source works too, stripping any
`localpath`) and opens a labeled GitHub issue; a workflow converts it to a PR for review.
**Helper scripts** a fragment declares (`[[sources.build]].assets`, or a tool source's
top-level `assets`) are
bundled into the issue as a base64 `tar.gz` and unpacked by the workflow into
**individual files** next to the fragment in the PR — so a tool's post-processing
scripts travel with it. (GitHub's API can't attach real files, so the tarball rides in
the issue body; keep assets to text scripts — large/binary data belongs in a `url`/
`setup` download, not an asset.) Without a token, `submit` writes the issue body to a
file to paste manually. Submission only targets the public registry; fetching works
with any HTTPS registry.

## Commands

```
config:
  init                           scaffold config.toml + a starter snapshot
  configure | edit               interactive editor: snapshots, sources, annotations
  snapshot add <name> [--from S] create a snapshot (optionally copy another)
  snapshot list                  list snapshots
  source add <snapshot> [...]    add a source fragment (prompts when flags omitted)
  annotation add <snapshot> [...] add an annotation (--default to apply by default)
  annotation list <snapshot>     list annotations + the default set
  download [--source N] [-j N]   fetch the snapshot's sources + tabix-index them
  registry list|pull-snapshot|add-source|submit

annotate (no flag = default-marked annotations; --all or -a name,... to choose):
  annotate [--all|-a k,...] [--format tab|vcf|json|text] [-o FILE] <vcf|locus...>
                                    annotate loci/VCF; TSV by default, -o writes to FILE
  versions                          show the active snapshot
  version                           print the cgtag version
```

Run with `-home DIR` to point at a specific `CGTAG_HOME`, or `-snapshot NAME` to
override the default snapshot.

## How annotation works

Both paths run on the [`hts`](https://github.com/compgenlab/hts) `vcf/annotate`
engine:

- **Loci → values** (`annotate <locus>`): each source becomes a tabix annotator;
  per-locus results are merged and **memoized** in SQLite. Repeat queries are pure
  cache reads. Any `type = "tool"` sources referenced by the selected annotations are
  run over the requested loci too (per-locus tool cache, as below), then overlaid
  like a data source.
- **VCF → annotated VCF** (`annotate --format vcf -o`): a streaming pipeline applies the
  snapshot's sources, its builtin annotations (`CG_*` tags), and any `type = "tool"`
  sources (run locally or via a batch scheduler, output consumed as a source).

An external tool's raw output is **cached per locus** (keyed by `name:version`), so on
later runs the tool executes only on loci it hasn't seen — the output file is rebuilt
from the cache and the tabix annotator consumes it unchanged.

> **Cache note.** The locus cache keys on the locus, not on the annotation set: once a
> locus is cached it is treated as fully annotated. So a locus computed *before* a new
> source was added (or before a tool source ran on the locus path) won't gain the new
> values until it is recomputed — query a fresh locus, or clear the cache DB
> (`database.path`) to force a re-annotation.

## Roadmap

- Postgres backend for shared/server deployments.
- A `POST /annotate` REST endpoint (loci/VCF → annotations).
- GTF gene-model annotation and first-class VEP (currently: run VEP as an external
  tool emitting a tab/vcf file, then consume it as a source).

## Development

```sh
make build      # bin/cgtag
make test       # go test -race ./...
make vet
make cross      # release tarballs (linux,darwin × amd64,arm64)
```

**Supported platforms:** Linux and macOS, on both `amd64` and `arm64`. Because
cgtag is pure Go, `make cross` static-cross-compiles all four from any host with no
C toolchain. (`make dist-windows` builds an opt-in windows/amd64 zip.)

## License

Not yet chosen. (TODO: add a `LICENSE` file before distributing.)
