package storage

import "time"

// primitiveDescriptor declares the minimal metadata doctor and cleanup
// need to reason about a primitive's table without knowing its
// public surface. New primitives append a row to `primitives` below.
type primitiveDescriptor struct {
	Name             string
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
		Name:             "kv",
		Table:            "kv",
		CreatedAtCol:     "created_at",
		UpdatedAtCol:     "updated_at",
		ExpiresAtCol:     "expires_at",
		DefaultRetention: 0,
	},
}
