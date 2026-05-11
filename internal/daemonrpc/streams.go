package daemonrpc

import (
	"context"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
)

type ServerEventStream interface {
	Context() context.Context
	Send(*daemonv1.SubscribeServerEventsResponse) error
}

type ActivityStream interface {
	Context() context.Context
	Send(*daemonv1.SubscribeActivityResponse) error
}

type AgentRunStream interface {
	Context() context.Context
	Recv() (*daemonv1.AgentRunEvent, error)
	SendAndClose(*daemonv1.ReportAgentRunResponse) error
}

type ProxyStream interface {
	Context() context.Context
	Recv() (*daemonv1.ProxyFrame, error)
	Send(*daemonv1.ProxyFrame) error
}
