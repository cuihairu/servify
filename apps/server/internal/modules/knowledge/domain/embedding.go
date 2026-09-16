package domain

import (
	"database/sql/driver"
	"strings"

	"github.com/pgvector/pgvector-go"
)

// Embedding wraps pgvector.Vector so that knowledge docs without an embedding
// round-trip cleanly. A zero pgvector.Vector serializes to the "[]" literal,
// whose scan fails (strconv.ParseFloat on the empty element), and an unset
// column may also surface as NULL or an empty string depending on the driver.
// Embedding tolerates nil / "" / "[]" on scan and writes NULL for empty
// vectors so docs are not rejected on Postgres vector columns either.
type Embedding struct {
	pgvector.Vector
}

// NewEmbedding wraps a float32 slice into an Embedding.
func NewEmbedding(vec []float32) Embedding {
	return Embedding{Vector: pgvector.NewVector(vec)}
}

// Scan implements sql.Scanner, tolerating nil, "", and "[]" as empty vectors.
func (e *Embedding) Scan(src any) error {
	switch src {
	case nil:
		e.Vector = pgvector.Vector{}
		return nil
	}
	switch s := src.(type) {
	case string:
		if isEmptyVectorLiteral(s) {
			e.Vector = pgvector.Vector{}
			return nil
		}
		return e.Vector.Scan(src)
	case []byte:
		if isEmptyVectorLiteral(string(s)) {
			e.Vector = pgvector.Vector{}
			return nil
		}
		return e.Vector.Scan(src)
	default:
		return e.Vector.Scan(src)
	}
}

// Value implements driver.Valuer. Empty vectors are written as NULL so they do
// not produce an invalid "[]" literal or trip Postgres vector column checks.
func (e Embedding) Value() (driver.Value, error) {
	if len(e.Vector.Slice()) == 0 {
		return nil, nil
	}
	return e.Vector.Value()
}

func isEmptyVectorLiteral(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || t == "[]"
}
