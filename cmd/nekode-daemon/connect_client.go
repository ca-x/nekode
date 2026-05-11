package main

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/gen/go/nekode/daemon/v1/daemonv1connect"
	"golang.org/x/net/http2"
)

type authContextKey struct{}

type daemonControlClient interface {
	runSupervisorClient
	RegisterComputer(context.Context, *daemonv1.RegisterComputerRequest) (*daemonv1.RegisterComputerResponse, error)
	HeartbeatComputer(context.Context, *daemonv1.HeartbeatComputerRequest) (*daemonv1.HeartbeatComputerResponse, error)
	SyncComputerInventory(context.Context, *daemonv1.SyncComputerInventoryRequest) (*daemonv1.SyncComputerInventoryResponse, error)
	SubscribeServerEvents(context.Context, *daemonv1.SubscribeServerEventsRequest) (serverEventClientStream, error)
	AcknowledgeServerEvents(context.Context, *daemonv1.AcknowledgeServerEventsRequest) (*daemonv1.AcknowledgeServerEventsResponse, error)
	ProxyExchange(context.Context) (proxyExchangeClientStream, error)
}

type serverEventClientStream interface {
	Recv() (*daemonv1.SubscribeServerEventsResponse, error)
}

type reportAgentRunClient interface {
	Send(*daemonv1.AgentRunEvent) error
	CloseAndRecv() (*daemonv1.ReportAgentRunResponse, error)
	Close() error
}

type proxyExchangeClientStream interface {
	Send(*daemonv1.ProxyFrame) error
	Recv() (*daemonv1.ProxyFrame, error)
}

type connectDaemonClient struct {
	client daemonv1connect.DaemonControlServiceClient
}

func newConnectDaemonClient(httpClient connect.HTTPClient, baseURL string) daemonControlClient {
	return connectDaemonClient{client: daemonv1connect.NewDaemonControlServiceClient(httpClient, baseURL)}
}

func defaultConnectHTTPClient() connect.HTTPClient {
	return &http.Client{Transport: &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

func connectReq[T any](ctx context.Context, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if token, _ := ctx.Value(authContextKey{}).(string); strings.TrimSpace(token) != "" {
		req.Header().Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	return req
}

func connectMsg[T any](resp *connect.Response[T], err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c connectDaemonClient) RegisterComputer(ctx context.Context, req *daemonv1.RegisterComputerRequest) (*daemonv1.RegisterComputerResponse, error) {
	return connectMsg(c.client.RegisterComputer(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) HeartbeatComputer(ctx context.Context, req *daemonv1.HeartbeatComputerRequest) (*daemonv1.HeartbeatComputerResponse, error) {
	return connectMsg(c.client.HeartbeatComputer(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) SyncComputerInventory(ctx context.Context, req *daemonv1.SyncComputerInventoryRequest) (*daemonv1.SyncComputerInventoryResponse, error) {
	return connectMsg(c.client.SyncComputerInventory(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) AcquireStartPermit(ctx context.Context, req *daemonv1.AcquireStartPermitRequest) (*daemonv1.AcquireStartPermitResponse, error) {
	return connectMsg(c.client.AcquireStartPermit(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) ReleaseStartPermit(ctx context.Context, req *daemonv1.ReleaseStartPermitRequest) (*daemonv1.ReleaseStartPermitResponse, error) {
	return connectMsg(c.client.ReleaseStartPermit(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) FetchAssignedRuns(ctx context.Context, req *daemonv1.FetchAssignedRunsRequest) (*daemonv1.FetchAssignedRunsResponse, error) {
	return connectMsg(c.client.FetchAssignedRuns(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) GetLaunchPromptSnapshot(ctx context.Context, req *daemonv1.GetLaunchPromptSnapshotRequest) (*daemonv1.GetLaunchPromptSnapshotResponse, error) {
	return connectMsg(c.client.GetLaunchPromptSnapshot(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) UpdateRunStatus(ctx context.Context, req *daemonv1.UpdateRunStatusRequest) (*daemonv1.UpdateRunStatusResponse, error) {
	return connectMsg(c.client.UpdateRunStatus(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) RenewRunLease(ctx context.Context, req *daemonv1.RenewRunLeaseRequest) (*daemonv1.RenewRunLeaseResponse, error) {
	return connectMsg(c.client.RenewRunLease(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) AppendRunStep(ctx context.Context, req *daemonv1.AppendRunStepRequest) (*daemonv1.AppendRunStepResponse, error) {
	return connectMsg(c.client.AppendRunStep(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) UpdateAgentStatus(ctx context.Context, req *daemonv1.UpdateAgentStatusRequest) (*daemonv1.UpdateAgentStatusResponse, error) {
	return connectMsg(c.client.UpdateAgentStatus(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) SendMessage(ctx context.Context, req *daemonv1.SendMessageRequest) (*daemonv1.SendMessageResponse, error) {
	return connectMsg(c.client.SendMessage(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) LogActivity(ctx context.Context, req *daemonv1.LogActivityRequest) (*daemonv1.LogActivityResponse, error) {
	return connectMsg(c.client.LogActivity(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) AcknowledgeServerEvents(ctx context.Context, req *daemonv1.AcknowledgeServerEventsRequest) (*daemonv1.AcknowledgeServerEventsResponse, error) {
	return connectMsg(c.client.AcknowledgeServerEvents(ctx, connectReq(ctx, req)))
}

func (c connectDaemonClient) SubscribeServerEvents(ctx context.Context, req *daemonv1.SubscribeServerEventsRequest) (serverEventClientStream, error) {
	stream, err := c.client.SubscribeServerEvents(ctx, connectReq(ctx, req))
	if err != nil {
		return nil, err
	}
	return connectServerEventStream{stream: stream}, nil
}

func (c connectDaemonClient) ReportAgentRun(ctx context.Context) (reportAgentRunClient, error) {
	stream := c.client.ReportAgentRun(ctx)
	if token, _ := ctx.Value(authContextKey{}).(string); strings.TrimSpace(token) != "" {
		stream.RequestHeader().Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	return connectReportAgentRunStream{stream: stream}, nil
}

func (c connectDaemonClient) ProxyExchange(ctx context.Context) (proxyExchangeClientStream, error) {
	stream := c.client.ProxyExchange(ctx)
	if token, _ := ctx.Value(authContextKey{}).(string); strings.TrimSpace(token) != "" {
		stream.RequestHeader().Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	return connectProxyExchangeStream{stream: stream}, nil
}

type connectServerEventStream struct {
	stream *connect.ServerStreamForClient[daemonv1.SubscribeServerEventsResponse]
}

func (s connectServerEventStream) Recv() (*daemonv1.SubscribeServerEventsResponse, error) {
	if !s.stream.Receive() {
		if err := s.stream.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return s.stream.Msg(), nil
}

type connectReportAgentRunStream struct {
	stream *connect.ClientStreamForClient[daemonv1.AgentRunEvent, daemonv1.ReportAgentRunResponse]
}

func (s connectReportAgentRunStream) Send(event *daemonv1.AgentRunEvent) error {
	return s.stream.Send(event)
}

func (s connectReportAgentRunStream) CloseAndRecv() (*daemonv1.ReportAgentRunResponse, error) {
	return connectMsg(s.stream.CloseAndReceive())
}

func (s connectReportAgentRunStream) Close() error {
	conn, err := s.stream.Conn()
	if err != nil {
		return err
	}
	return conn.CloseRequest()
}

type connectProxyExchangeStream struct {
	stream *connect.BidiStreamForClient[daemonv1.ProxyFrame, daemonv1.ProxyFrame]
}

func (s connectProxyExchangeStream) Send(frame *daemonv1.ProxyFrame) error {
	return s.stream.Send(frame)
}

func (s connectProxyExchangeStream) Recv() (*daemonv1.ProxyFrame, error) {
	return s.stream.Receive()
}
