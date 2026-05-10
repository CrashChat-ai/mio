package worker

import (
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

func TestNoopProcessorReturnsNil(t *testing.T) {
	if err := (NoopProcessor{}).Process(t.Context(), &miov1.Message{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
