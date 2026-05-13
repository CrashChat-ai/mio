package nats

import (
	"errors"
	"testing"
)

// TestCheckProdStorage covers the full env × storage matrix that the
// all-in-one binary's boot path delegates to. The matrix is small and
// closed (env ∈ {"", dev, staging, prod} × storage ∈ {memory, file});
// asserting every cell catches both regressions and accidental new
// branches that would broaden the guard rail.
func TestCheckProdStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		env       string
		storage   string
		wantErr   error // nil = no error
		wantWarn  bool  // expected WarnSingleNode value
	}{
		{
			name:    "empty env defaults to dev (memory allowed)",
			env:     "",
			storage: "memory",
		},
		{
			name:    "empty env defaults to dev (file allowed)",
			env:     "",
			storage: "file",
		},
		{
			name:    "dev + memory",
			env:     "dev",
			storage: "memory",
		},
		{
			name:    "dev + file",
			env:     "dev",
			storage: "file",
		},
		{
			name:    "staging + memory",
			env:     "staging",
			storage: "memory",
		},
		{
			name:    "staging + file",
			env:     "staging",
			storage: "file",
		},
		{
			name:    "prod + memory rejected",
			env:     "prod",
			storage: "memory",
			wantErr: ErrProdMemoryForbidden,
		},
		{
			name:     "prod + file warns single-node",
			env:      "prod",
			storage:  "file",
			wantWarn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec, err := CheckProdStorage(tt.env, tt.storage)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err: got %v want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if dec.WarnSingleNode != tt.wantWarn {
				t.Errorf("WarnSingleNode: got %v want %v", dec.WarnSingleNode, tt.wantWarn)
			}
		})
	}
}
