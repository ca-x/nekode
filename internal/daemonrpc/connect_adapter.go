package daemonrpc

import (
	"context"
	"io"

	"connectrpc.com/connect"
	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/gen/go/nekode/daemon/v1/daemonv1connect"
)

type connectHandler struct {
	daemonv1connect.UnimplementedDaemonControlServiceHandler
	server *Server
}

func NewConnectHandler(server *Server) daemonv1connect.DaemonControlServiceHandler {
	return &connectHandler{server: server}
}

func connectResp[T any](msg *T, err error) (*connect.Response[T], error) {
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(msg), nil
}

type connectServerStream[T any] struct {
	ctx    context.Context
	stream *connect.ServerStream[T]
}

func (s connectServerStream[T]) Context() context.Context {
	return s.ctx
}

func (s connectServerStream[T]) Send(msg *T) error {
	return s.stream.Send(msg)
}

type connectBidiStream[T any] struct {
	ctx    context.Context
	stream *connect.BidiStream[T, T]
}

func (s connectBidiStream[T]) Context() context.Context {
	return s.ctx
}

func (s connectBidiStream[T]) Send(msg *T) error {
	return s.stream.Send(msg)
}

func (s connectBidiStream[T]) Recv() (*T, error) {
	return s.stream.Receive()
}

type connectAgentRunStream struct {
	ctx    context.Context
	stream *connect.ClientStream[daemonv1.AgentRunEvent]
}

func (s connectAgentRunStream) Context() context.Context {
	return s.ctx
}

func (s connectAgentRunStream) Recv() (*daemonv1.AgentRunEvent, error) {
	if !s.stream.Receive() {
		if err := s.stream.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return s.stream.Msg(), nil
}

type agentRunReceiver interface {
	Context() context.Context
	Recv() (*daemonv1.AgentRunEvent, error)
}

type agentRunResponseStream struct {
	inner    agentRunReceiver
	response *daemonv1.ReportAgentRunResponse
}

func (s agentRunResponseStream) Context() context.Context {
	return s.inner.Context()
}

func (s agentRunResponseStream) Recv() (*daemonv1.AgentRunEvent, error) {
	return s.inner.Recv()
}

func (s agentRunResponseStream) SendAndClose(resp *daemonv1.ReportAgentRunResponse) error {
	if resp == nil || s.response == nil {
		return nil
	}
	*s.response = *resp
	return nil
}

func (h *connectHandler) RegisterComputer(ctx context.Context, req *connect.Request[daemonv1.RegisterComputerRequest]) (*connect.Response[daemonv1.RegisterComputerResponse], error) {
	return connectResp(h.server.RegisterComputer(ctx, req.Msg))
}

func (h *connectHandler) HeartbeatComputer(ctx context.Context, req *connect.Request[daemonv1.HeartbeatComputerRequest]) (*connect.Response[daemonv1.HeartbeatComputerResponse], error) {
	return connectResp(h.server.HeartbeatComputer(ctx, req.Msg))
}

func (h *connectHandler) SyncComputerInventory(ctx context.Context, req *connect.Request[daemonv1.SyncComputerInventoryRequest]) (*connect.Response[daemonv1.SyncComputerInventoryResponse], error) {
	return connectResp(h.server.SyncComputerInventory(ctx, req.Msg))
}

func (h *connectHandler) ListRuntimePresets(ctx context.Context, req *connect.Request[daemonv1.ListRuntimePresetsRequest]) (*connect.Response[daemonv1.ListRuntimePresetsResponse], error) {
	return connectResp(h.server.ListRuntimePresets(ctx, req.Msg))
}

func (h *connectHandler) AcquireStartPermit(ctx context.Context, req *connect.Request[daemonv1.AcquireStartPermitRequest]) (*connect.Response[daemonv1.AcquireStartPermitResponse], error) {
	return connectResp(h.server.AcquireStartPermit(ctx, req.Msg))
}

func (h *connectHandler) ReleaseStartPermit(ctx context.Context, req *connect.Request[daemonv1.ReleaseStartPermitRequest]) (*connect.Response[daemonv1.ReleaseStartPermitResponse], error) {
	return connectResp(h.server.ReleaseStartPermit(ctx, req.Msg))
}

func (h *connectHandler) FetchAssignedRuns(ctx context.Context, req *connect.Request[daemonv1.FetchAssignedRunsRequest]) (*connect.Response[daemonv1.FetchAssignedRunsResponse], error) {
	return connectResp(h.server.FetchAssignedRuns(ctx, req.Msg))
}

func (h *connectHandler) GetLaunchPromptSnapshot(ctx context.Context, req *connect.Request[daemonv1.GetLaunchPromptSnapshotRequest]) (*connect.Response[daemonv1.GetLaunchPromptSnapshotResponse], error) {
	return connectResp(h.server.GetLaunchPromptSnapshot(ctx, req.Msg))
}

func (h *connectHandler) UpdateRunStatus(ctx context.Context, req *connect.Request[daemonv1.UpdateRunStatusRequest]) (*connect.Response[daemonv1.UpdateRunStatusResponse], error) {
	return connectResp(h.server.UpdateRunStatus(ctx, req.Msg))
}

func (h *connectHandler) RenewRunLease(ctx context.Context, req *connect.Request[daemonv1.RenewRunLeaseRequest]) (*connect.Response[daemonv1.RenewRunLeaseResponse], error) {
	return connectResp(h.server.RenewRunLease(ctx, req.Msg))
}

func (h *connectHandler) AppendRunStep(ctx context.Context, req *connect.Request[daemonv1.AppendRunStepRequest]) (*connect.Response[daemonv1.AppendRunStepResponse], error) {
	return connectResp(h.server.AppendRunStep(ctx, req.Msg))
}

func (h *connectHandler) ListRuns(ctx context.Context, req *connect.Request[daemonv1.ListRunsRequest]) (*connect.Response[daemonv1.ListRunsResponse], error) {
	return connectResp(h.server.ListRuns(ctx, req.Msg))
}

func (h *connectHandler) GetRun(ctx context.Context, req *connect.Request[daemonv1.GetRunRequest]) (*connect.Response[daemonv1.GetRunResponse], error) {
	return connectResp(h.server.GetRun(ctx, req.Msg))
}

func (h *connectHandler) ListChannels(ctx context.Context, req *connect.Request[daemonv1.ListChannelsRequest]) (*connect.Response[daemonv1.ListChannelsResponse], error) {
	return connectResp(h.server.ListChannels(ctx, req.Msg))
}

func (h *connectHandler) ListInteractionEndpoints(ctx context.Context, req *connect.Request[daemonv1.ListInteractionEndpointsRequest]) (*connect.Response[daemonv1.ListInteractionEndpointsResponse], error) {
	return connectResp(h.server.ListInteractionEndpoints(ctx, req.Msg))
}

func (h *connectHandler) ReadMessages(ctx context.Context, req *connect.Request[daemonv1.ReadMessagesRequest]) (*connect.Response[daemonv1.ReadMessagesResponse], error) {
	return connectResp(h.server.ReadMessages(ctx, req.Msg))
}

func (h *connectHandler) SearchMessages(ctx context.Context, req *connect.Request[daemonv1.SearchMessagesRequest]) (*connect.Response[daemonv1.SearchMessagesResponse], error) {
	return connectResp(h.server.SearchMessages(ctx, req.Msg))
}

func (h *connectHandler) SendMessage(ctx context.Context, req *connect.Request[daemonv1.SendMessageRequest]) (*connect.Response[daemonv1.SendMessageResponse], error) {
	return connectResp(h.server.SendMessage(ctx, req.Msg))
}

func (h *connectHandler) SaveMessage(ctx context.Context, req *connect.Request[daemonv1.SaveMessageRequest]) (*connect.Response[daemonv1.SaveMessageResponse], error) {
	return connectResp(h.server.SaveMessage(ctx, req.Msg))
}

func (h *connectHandler) UnsaveMessage(ctx context.Context, req *connect.Request[daemonv1.UnsaveMessageRequest]) (*connect.Response[daemonv1.UnsaveMessageResponse], error) {
	return connectResp(h.server.UnsaveMessage(ctx, req.Msg))
}

func (h *connectHandler) ListSavedMessages(ctx context.Context, req *connect.Request[daemonv1.ListSavedMessagesRequest]) (*connect.Response[daemonv1.ListSavedMessagesResponse], error) {
	return connectResp(h.server.ListSavedMessages(ctx, req.Msg))
}

func (h *connectHandler) CreateCollaborationTask(ctx context.Context, req *connect.Request[daemonv1.CreateCollaborationTaskRequest]) (*connect.Response[daemonv1.CreateCollaborationTaskResponse], error) {
	return connectResp(h.server.CreateCollaborationTask(ctx, req.Msg))
}

func (h *connectHandler) GetTask(ctx context.Context, req *connect.Request[daemonv1.GetTaskRequest]) (*connect.Response[daemonv1.GetTaskResponse], error) {
	return connectResp(h.server.GetTask(ctx, req.Msg))
}

func (h *connectHandler) UpdateTask(ctx context.Context, req *connect.Request[daemonv1.UpdateTaskRequest]) (*connect.Response[daemonv1.UpdateTaskResponse], error) {
	return connectResp(h.server.UpdateTask(ctx, req.Msg))
}

func (h *connectHandler) ListCollaborationTasks(ctx context.Context, req *connect.Request[daemonv1.ListCollaborationTasksRequest]) (*connect.Response[daemonv1.ListCollaborationTasksResponse], error) {
	return connectResp(h.server.ListCollaborationTasks(ctx, req.Msg))
}

func (h *connectHandler) ListTaskBoard(ctx context.Context, req *connect.Request[daemonv1.ListTaskBoardRequest]) (*connect.Response[daemonv1.ListTaskBoardResponse], error) {
	return connectResp(h.server.ListTaskBoard(ctx, req.Msg))
}

func (h *connectHandler) ClaimCollaborationTask(ctx context.Context, req *connect.Request[daemonv1.ClaimCollaborationTaskRequest]) (*connect.Response[daemonv1.ClaimCollaborationTaskResponse], error) {
	return connectResp(h.server.ClaimCollaborationTask(ctx, req.Msg))
}

func (h *connectHandler) ScheduleReminder(ctx context.Context, req *connect.Request[daemonv1.ScheduleReminderRequest]) (*connect.Response[daemonv1.ScheduleReminderResponse], error) {
	return connectResp(h.server.ScheduleReminder(ctx, req.Msg))
}

func (h *connectHandler) ListReminders(ctx context.Context, req *connect.Request[daemonv1.ListRemindersRequest]) (*connect.Response[daemonv1.ListRemindersResponse], error) {
	return connectResp(h.server.ListReminders(ctx, req.Msg))
}

func (h *connectHandler) CancelReminder(ctx context.Context, req *connect.Request[daemonv1.CancelReminderRequest]) (*connect.Response[daemonv1.CancelReminderResponse], error) {
	return connectResp(h.server.CancelReminder(ctx, req.Msg))
}

func (h *connectHandler) SnoozeReminder(ctx context.Context, req *connect.Request[daemonv1.SnoozeReminderRequest]) (*connect.Response[daemonv1.SnoozeReminderResponse], error) {
	return connectResp(h.server.SnoozeReminder(ctx, req.Msg))
}

func (h *connectHandler) UpdateReminder(ctx context.Context, req *connect.Request[daemonv1.UpdateReminderRequest]) (*connect.Response[daemonv1.UpdateReminderResponse], error) {
	return connectResp(h.server.UpdateReminder(ctx, req.Msg))
}

func (h *connectHandler) GetReminderLog(ctx context.Context, req *connect.Request[daemonv1.GetReminderLogRequest]) (*connect.Response[daemonv1.GetReminderLogResponse], error) {
	return connectResp(h.server.GetReminderLog(ctx, req.Msg))
}

func (h *connectHandler) UpdateAgentStatus(ctx context.Context, req *connect.Request[daemonv1.UpdateAgentStatusRequest]) (*connect.Response[daemonv1.UpdateAgentStatusResponse], error) {
	return connectResp(h.server.UpdateAgentStatus(ctx, req.Msg))
}

func (h *connectHandler) ListAgentStatuses(ctx context.Context, req *connect.Request[daemonv1.ListAgentStatusesRequest]) (*connect.Response[daemonv1.ListAgentStatusesResponse], error) {
	return connectResp(h.server.ListAgentStatuses(ctx, req.Msg))
}

func (h *connectHandler) ListAgentProfiles(ctx context.Context, req *connect.Request[daemonv1.ListAgentProfilesRequest]) (*connect.Response[daemonv1.ListAgentProfilesResponse], error) {
	return connectResp(h.server.ListAgentProfiles(ctx, req.Msg))
}

func (h *connectHandler) ControlAgent(ctx context.Context, req *connect.Request[daemonv1.ControlAgentRequest]) (*connect.Response[daemonv1.ControlAgentResponse], error) {
	return connectResp(h.server.ControlAgent(ctx, req.Msg))
}

func (h *connectHandler) SendAgentDirectMessage(ctx context.Context, req *connect.Request[daemonv1.SendAgentDirectMessageRequest]) (*connect.Response[daemonv1.SendAgentDirectMessageResponse], error) {
	return connectResp(h.server.SendAgentDirectMessage(ctx, req.Msg))
}

func (h *connectHandler) AcknowledgeServerEvents(ctx context.Context, req *connect.Request[daemonv1.AcknowledgeServerEventsRequest]) (*connect.Response[daemonv1.AcknowledgeServerEventsResponse], error) {
	return connectResp(h.server.AcknowledgeServerEvents(ctx, req.Msg))
}

func (h *connectHandler) LogActivity(ctx context.Context, req *connect.Request[daemonv1.LogActivityRequest]) (*connect.Response[daemonv1.LogActivityResponse], error) {
	return connectResp(h.server.LogActivity(ctx, req.Msg))
}

func (h *connectHandler) ListActivity(ctx context.Context, req *connect.Request[daemonv1.ListActivityRequest]) (*connect.Response[daemonv1.ListActivityResponse], error) {
	return connectResp(h.server.ListActivity(ctx, req.Msg))
}

func (h *connectHandler) AcknowledgeActivityEvents(ctx context.Context, req *connect.Request[daemonv1.AcknowledgeActivityEventsRequest]) (*connect.Response[daemonv1.AcknowledgeActivityEventsResponse], error) {
	return connectResp(h.server.AcknowledgeActivityEvents(ctx, req.Msg))
}

func (h *connectHandler) ListEventsSince(ctx context.Context, req *connect.Request[daemonv1.ListEventsSinceRequest]) (*connect.Response[daemonv1.ListEventsSinceResponse], error) {
	return connectResp(h.server.ListEventsSince(ctx, req.Msg))
}

func (h *connectHandler) CreateTunnel(ctx context.Context, req *connect.Request[daemonv1.CreateTunnelRequest]) (*connect.Response[daemonv1.CreateTunnelResponse], error) {
	return connectResp(h.server.CreateTunnel(ctx, req.Msg))
}

func (h *connectHandler) ListTunnels(ctx context.Context, req *connect.Request[daemonv1.ListTunnelsRequest]) (*connect.Response[daemonv1.ListTunnelsResponse], error) {
	return connectResp(h.server.ListTunnels(ctx, req.Msg))
}

func (h *connectHandler) ApproveTunnel(ctx context.Context, req *connect.Request[daemonv1.ApproveTunnelRequest]) (*connect.Response[daemonv1.ApproveTunnelResponse], error) {
	return connectResp(h.server.ApproveTunnel(ctx, req.Msg))
}

func (h *connectHandler) RejectTunnel(ctx context.Context, req *connect.Request[daemonv1.RejectTunnelRequest]) (*connect.Response[daemonv1.RejectTunnelResponse], error) {
	return connectResp(h.server.RejectTunnel(ctx, req.Msg))
}

func (h *connectHandler) CloseTunnel(ctx context.Context, req *connect.Request[daemonv1.CloseTunnelRequest]) (*connect.Response[daemonv1.CloseTunnelResponse], error) {
	return connectResp(h.server.CloseTunnel(ctx, req.Msg))
}

func (h *connectHandler) ListChannelDecisions(ctx context.Context, req *connect.Request[daemonv1.ListChannelDecisionsRequest]) (*connect.Response[daemonv1.ListChannelDecisionsResponse], error) {
	return connectResp(h.server.ListChannelDecisions(ctx, req.Msg))
}

func (h *connectHandler) ProposeChannelDecision(ctx context.Context, req *connect.Request[daemonv1.ProposeChannelDecisionRequest]) (*connect.Response[daemonv1.ProposeChannelDecisionResponse], error) {
	return connectResp(h.server.ProposeChannelDecision(ctx, req.Msg))
}

func (h *connectHandler) VoteChannelDecision(ctx context.Context, req *connect.Request[daemonv1.VoteChannelDecisionRequest]) (*connect.Response[daemonv1.VoteChannelDecisionResponse], error) {
	return connectResp(h.server.VoteChannelDecision(ctx, req.Msg))
}

func (h *connectHandler) RatifyChannelDecision(ctx context.Context, req *connect.Request[daemonv1.RatifyChannelDecisionRequest]) (*connect.Response[daemonv1.RatifyChannelDecisionResponse], error) {
	return connectResp(h.server.RatifyChannelDecision(ctx, req.Msg))
}

func (h *connectHandler) RetireChannelDecision(ctx context.Context, req *connect.Request[daemonv1.RetireChannelDecisionRequest]) (*connect.Response[daemonv1.RetireChannelDecisionResponse], error) {
	return connectResp(h.server.RetireChannelDecision(ctx, req.Msg))
}

func (h *connectHandler) ListDecisionVotes(ctx context.Context, req *connect.Request[daemonv1.ListDecisionVotesRequest]) (*connect.Response[daemonv1.ListDecisionVotesResponse], error) {
	return connectResp(h.server.ListDecisionVotes(ctx, req.Msg))
}

func (h *connectHandler) ReportAgentRun(ctx context.Context, stream *connect.ClientStream[daemonv1.AgentRunEvent]) (*connect.Response[daemonv1.ReportAgentRunResponse], error) {
	wrapped := connectAgentRunStream{ctx: ctx, stream: stream}
	resp := &daemonv1.ReportAgentRunResponse{}
	if err := h.server.ReportAgentRun(agentRunResponseStream{inner: wrapped, response: resp}); err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}

func (h *connectHandler) ListAgentRuns(ctx context.Context, req *connect.Request[daemonv1.ListAgentRunsRequest]) (*connect.Response[daemonv1.ListAgentRunsResponse], error) {
	return connectResp(h.server.ListAgentRuns(ctx, req.Msg))
}

func (h *connectHandler) GetAgentRun(ctx context.Context, req *connect.Request[daemonv1.GetAgentRunRequest]) (*connect.Response[daemonv1.GetAgentRunResponse], error) {
	return connectResp(h.server.GetAgentRun(ctx, req.Msg))
}

func (h *connectHandler) SearchAgentRuns(ctx context.Context, req *connect.Request[daemonv1.SearchAgentRunsRequest]) (*connect.Response[daemonv1.SearchAgentRunsResponse], error) {
	return connectResp(h.server.SearchAgentRuns(ctx, req.Msg))
}

func (h *connectHandler) SubscribeServerEvents(ctx context.Context, req *connect.Request[daemonv1.SubscribeServerEventsRequest], stream *connect.ServerStream[daemonv1.SubscribeServerEventsResponse]) error {
	return h.server.SubscribeServerEvents(req.Msg, connectServerStream[daemonv1.SubscribeServerEventsResponse]{ctx: ctx, stream: stream})
}

func (h *connectHandler) SubscribeActivity(ctx context.Context, req *connect.Request[daemonv1.SubscribeActivityRequest], stream *connect.ServerStream[daemonv1.SubscribeActivityResponse]) error {
	return h.server.SubscribeActivity(req.Msg, connectServerStream[daemonv1.SubscribeActivityResponse]{ctx: ctx, stream: stream})
}

func (h *connectHandler) ProxyExchange(ctx context.Context, stream *connect.BidiStream[daemonv1.ProxyFrame, daemonv1.ProxyFrame]) error {
	return h.server.ProxyExchange(connectBidiStream[daemonv1.ProxyFrame]{ctx: ctx, stream: stream})
}
