# Quick start

## Install

Requires Go 1.25+.

```sh
go install github.com/compgenlab/cganno/cmd/cganno@latest
```

cganno is pure Go (`CGO_ENABLED=0`) — the engine, the [`hts`](https://github.com/compgenlab/hts)
library, and the SQLite driver — so it needs no C toolchain and cross-compiles cleanly.

## 1. Initialize

```sh
export CGANNO_HOME=~/cganno        # base dir for config, data, cache, and the DB
cganno init                       # prompts for the annotations dir + starter snapshot name,
                                 # scaffolds config.toml + sources/ + a snapshot manifest,
                                 # then offers to open the configure TUI
```

See **[Overview](overview.md)** for what `init` writes and the config model.

## 2. Add a source and reference it from the snapshot

Author a source by hand, interactively with `cganno configure`, or on the command line:

```sh
cganno source add --name gnomad --version 4.1 \
  --url https://…/gnomad.genomes.v4.1.sites.vcf.bgz --format vcf --snapshot 2026-07
cganno annotation add --source gnomad:4.1 --name gnomad_af --field AF --type numeric
```

Or pull a ready-made source from a **[registry](registry.md)**:

```sh
cganno registry add-source clinvar:2026-01 --snapshot 2026-07
```

The [source types](sources.md) are data files (`vcf`/`tab`/`bed`/`gtf`), `builtin`
annotators, and `tool` sources (VEP/ANNOVAR).

## 3. Download the data

```sh
cganno download -j 4              # fetch + tabix-index the snapshot's sources (4 files at once)
```

For build sources this runs their recipe; for tool sources it pulls the container image and
runs one-time setup. See the **[lifecycle](lifecycle.md)**.

## 4. Annotate

```sh
# a single locus → TSV of the snapshot's default annotations (the default format):
cganno annotate chr1:115256529:T:C

# a whole VCF → a fully-annotated VCF (samples preserved):
cganno annotate --all --format vcf -o out.vcf in.vcf

# only the named annotations, as JSON, to a file:
cganno annotate -a clinvar_sig,gnomad_af --format json -o hits.json in.vcf
```

Which annotations apply is chosen by `--all`, `-a name[,name…]`, or (with neither) the
snapshot's `default_annotations`. Output is `tab` by default; see the other
**[output formats](io-formats.md#annotate-output)** (`vcf` / `json` / `text`).

## Next steps

- **[Overview](overview.md)** — config model, snapshots, the cache.
- **[Source types](sources.md)** — builtin / vcf / tabix / bed / gtf / tool.
- **[Lifecycle](lifecycle.md)** — download, build recipes, tool setup + steps.
- **[Input & output formats](io-formats.md)** — `input_format`, `--format`.
- **[Registry](registry.md)** — pulling and submitting sources.
