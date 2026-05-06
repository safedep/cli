package storage

import "time"

// PrimitiveName identifies a storage primitive in cross-cutting APIs
// (cleanup retention overrides, doctor stats, config keys). It is a
// named string so typos at the API boundary become compile-time errors
// and the set of valid values is discoverable via the exported
// constants below.
type PrimitiveName string

const (
	PrimitiveKV PrimitiveName = "kv"
)

// primitiveDescriptor declares the minimal metadata doctor and cleanup
// need to reason about a primitive's table without knowing its
// public surface. New primitives append a row to `primitives` below.
type primitiveDescriptor struct {
	Name             PrimitiveName
	Table            string
	CreatedAtCol     string
	UpdatedAtCol     string
	ExpiresAtCol     string // empty when the table has no TTL column
	DefaultRetention time.Duration
}

// primitives is the registered descriptor table. Doctor and cleanup
// iterate this slice; adding a primitive is a one-line append plus
// implementing its public surface.
var primitives = []primitiveDescriptor{
	{
		Name:             PrimitiveKV,
		Table:            "kv",
		CreatedAtCol:     "created_at",
		UpdatedAtCol:     "updated_at",
		ExpiresAtCol:     "expires_at",
		DefaultRetention: 0,
	},
}
