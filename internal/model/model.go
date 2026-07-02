// Package model holds the core domain types shared across the engine, store,
// and annotator. Loci are the unit of reference data; annotations are stored as
// (locus, data_source, key, value) rows (an EAV layout, see internal/store).
package model

import (
	"fmt"
	"strconv"
)

// Locus is a normalized variant site. Assembly is a property of the whole
// database (pinned in config), so it is not part of the per-row key.
type Locus struct {
	Chrom string
	Pos   int64
	Ref   string
	Alt   string
}

// Key is a stable string identity for a locus, used to group annotation rows.
func (l Locus) Key() string {
	return fmt.Sprintf("%s:%d:%s:%s", l.Chrom, l.Pos, l.Ref, l.Alt)
}

func (l Locus) String() string { return l.Key() }

// Value is a single annotation value. Numeric values populate Num (and set
// IsNum); everything else is Str. This mirrors the value_num/value_text split
// in the store, which keeps numeric filters indexable on every backend.
type Value struct {
	Str   string
	Num   float64
	IsNum bool
}

// Text builds a string-valued Value.
func Text(s string) Value { return Value{Str: s} }

// Number builds a numeric-valued Value.
func Number(f float64) Value { return Value{Num: f, IsNum: true} }

func (v Value) String() string {
	if v.IsNum {
		return strconv.FormatFloat(v.Num, 'g', -1, 64)
	}
	return v.Str
}

// AnnRow is one EAV annotation cell: a value for a key on a locus, attributed
// to a specific (versioned) data source.
type AnnRow struct {
	Locus      Locus
	DataSource string // data_source_id, e.g. "clinvar:2026-01"
	Key        string
	Value      Value
}

// DataSource is a pinned, versioned annotation source. ID = Name:Version.
// A source's Version is its own data version (e.g. clinvar "2026-01"), distinct
// from the top-level snapshot name. data_source_id = Name:Version.
type DataSource struct {
	Name    string
	Version string
	Path    string
}

// ID is the stable data_source_id used in annotation rows (name:version, a
// docker-style tag).
func (d DataSource) ID() string { return d.Name + ":" + d.Version }

// ToolLine is one cached raw output line from an external tool, tagged with the
// site it sits on so a rebuilt output file can be re-sorted by position. The
// line is stored verbatim (tab or VCF text) and re-matched to input records by
// the existing tabix annotator.
type ToolLine struct {
	Chrom string
	Pos   int64
	Line  string
}
