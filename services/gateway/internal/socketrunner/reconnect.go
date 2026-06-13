package socketrunner

import (
	"context"
	"time"

	"github.com/slack-go/slack/socketmode"
)

const reconnectBackoff = 5 * time.Second

// connectLoop runs the lib's RunContext with reconnect/backoff and fast-fails on
// permanent auth errors (port goclaw channel.go:173-190). A retry storm on
// revoked credentials never succeeds, so it stops instead of looping.
func (r *runner) connectLoop(ctx context.Context) {
	for {
		err := r.client.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			continue
		}
		if isPermanentAuthError(err.Error()) {
			r.logger.Error("socketrunner: permanent auth error, stopping reconnect", "err", err)
			return
		}
		r.logger.Warn("socketrunner: socket error, reconnecting", "backoff", r.backoff, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.backoff):
		}
	}
}

// liveClient adapts *socketmode.Client to socketClient.
type liveClient struct{ sm *socketmode.Client }

func (c *liveClient) Run(ctx context.Context) error   { return c.sm.RunContext(ctx) }
func (c *liveClient) Events() <-chan socketmode.Event { return c.sm.Events }
func (c *liveClient) Ack(req socketmode.Request)      { c.sm.Ack(req) }
