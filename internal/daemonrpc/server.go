package daemonrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/ca-x/nekode/internal/version"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const protocolVersion int32 = 1

type Server struct {
	daemonv1.UnimplementedDaemonControlServiceServer

	store      *storage.Store
	serverID   string
	serverName string

	mu          sync.Mutex
	computers   map[string]*computerState
	leases      map[string]*daemonv1.Lease
	statuses    map[string]*daemonv1.AgentStatusSnapshot
	runs        map[string]*daemonv1.Run
	runSteps    map[string][]*daemonv1.RunStep
	activities  []*daemonv1.ActivityRecord
	eventSeq    int64
	idempotency map[string]proto.Message
}

type computerState struct {
	info             *daemonv1.ComputerInfo
	inventory        *daemonv1.ComputerInventory
	inventoryVersion string
	lease            *daemonv1.Lease
	lastHeartbeat    int64
}

func New(store *storage.Store, serverID string) *Server {
	if strings.TrimSpace(serverID) == "" {
		serverID = storage.NewID("srv")
	}
	return &Server{
		store:       store,
		serverID:    serverID,
		serverName:  "Nekode",
		computers:   make(map[string]*computerState),
		leases:      make(map[string]*daemonv1.Lease),
		statuses:    make(map[string]*daemonv1.AgentStatusSnapshot),
		runs:        make(map[string]*daemonv1.Run),
		runSteps:    make(map[string][]*daemonv1.RunStep),
		idempotency: make(map[string]proto.Message),
	}
}

func (s *Server) ServerID() string {
	return s.serverID
}

func (s *Server) ServerName() string {
	return s.serverName
}

func (s *Server) ProtocolVersion() int32 {
	return protocolVersion
}

func (s *Server) RegisterComputer(ctx context.Context, req *daemonv1.RegisterComputerRequest) (*daemonv1.RegisterComputerResponse, error) {
	if resp, ok := replay(ctx, s, "RegisterComputer", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.RegisterComputerResponse {
		return &daemonv1.RegisterComputerResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "RegisterComputer", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	info := req.GetInfo()
	if info.GetComputerId() == "" || info.GetDaemonId() == "" {
		return nil, status.Error(codes.InvalidArgument, "computer_id and daemon_id are required")
	}
	now := unixNow()
	lease := newLease("computer", info.GetComputerId(), info.GetDaemonId(), 90)

	s.mu.Lock()
	s.computers[info.GetComputerId()] = &computerState{
		info:             proto.Clone(info).(*daemonv1.ComputerInfo),
		inventory:        cloneInventory(req.GetInventory()),
		inventoryVersion: "initial",
		lease:            lease,
		lastHeartbeat:    now,
	}
	s.leases[lease.LeaseId] = lease
	s.mu.Unlock()

	resp := &daemonv1.RegisterComputerResponse{Accepted: true, ServerTimeUnix: now, Lease: lease}
	remember(ctx, s, "RegisterComputer", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) HeartbeatComputer(_ context.Context, req *daemonv1.HeartbeatComputerRequest) (*daemonv1.HeartbeatComputerResponse, error) {
	info := req.GetInfo()
	computerID := info.GetComputerId()
	if computerID == "" {
		return nil, status.Error(codes.InvalidArgument, "computer_id is required")
	}
	now := unixNow()

	s.mu.Lock()
	state := s.computers[computerID]
	if state == nil {
		state = &computerState{}
		s.computers[computerID] = state
	}
	if info != nil {
		state.info = proto.Clone(info).(*daemonv1.ComputerInfo)
	}
	if req.GetInventoryFullSnapshot() {
		state.inventory = cloneInventory(req.GetInventory())
		state.inventoryVersion = req.GetInventoryVersion()
	}
	state.lastHeartbeat = now
	lease := state.lease
	if lease == nil || req.GetLeaseId() == "" || lease.GetLeaseId() != req.GetLeaseId() {
		holder := info.GetDaemonId()
		if holder == "" {
			holder = computerID
		}
		lease = newLease("computer", computerID, holder, 90)
		state.lease = lease
	}
	lease.ExpiresTimeUnix = now + 90
	s.leases[lease.LeaseId] = lease
	for _, snapshot := range req.GetAgentStatuses() {
		if snapshot.GetAgentId() == "" {
			continue
		}
		cp := proto.Clone(snapshot).(*daemonv1.AgentStatusSnapshot)
		if cp.UpdatedTimeUnix == 0 {
			cp.UpdatedTimeUnix = now
		}
		s.statuses[cp.AgentId] = cp
	}
	s.mu.Unlock()

	return &daemonv1.HeartbeatComputerResponse{
		Accepted:                  true,
		ServerTimeUnix:            now,
		NextHeartbeatAfterSeconds: 30,
		Lease:                     cloneLease(lease),
	}, nil
}

func (s *Server) SyncComputerInventory(ctx context.Context, req *daemonv1.SyncComputerInventoryRequest) (*daemonv1.SyncComputerInventoryResponse, error) {
	if resp, ok := replay(ctx, s, "SyncComputerInventory", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.SyncComputerInventoryResponse {
		return &daemonv1.SyncComputerInventoryResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "SyncComputerInventory", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	info := req.GetInfo()
	if info.GetComputerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "computer_id is required")
	}
	now := unixNow()
	versionID := req.GetInventoryVersion()
	if versionID == "" {
		versionID = strconv.FormatInt(now, 10)
	}
	s.mu.Lock()
	state := s.computers[info.GetComputerId()]
	if state == nil {
		state = &computerState{}
		s.computers[info.GetComputerId()] = state
	}
	state.info = proto.Clone(info).(*daemonv1.ComputerInfo)
	state.inventory = cloneInventory(req.GetInventory())
	state.inventoryVersion = versionID
	s.mu.Unlock()
	resp := &daemonv1.SyncComputerInventoryResponse{Accepted: true, InventoryVersion: versionID, ServerTimeUnix: now}
	remember(ctx, s, "SyncComputerInventory", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) AcquireStartPermit(ctx context.Context, req *daemonv1.AcquireStartPermitRequest) (*daemonv1.AcquireStartPermitResponse, error) {
	if resp, ok := replay(ctx, s, "AcquireStartPermit", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.AcquireStartPermitResponse {
		return &daemonv1.AcquireStartPermitResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "AcquireStartPermit", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetComputerId() == "" || req.GetAgentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "computer_id and agent_id are required")
	}
	ttl := req.GetPermitTtlSeconds()
	if ttl == 0 {
		ttl = 60
	}
	lease := newLease("start_permit", req.GetAgentId(), req.GetComputerId(), int64(ttl))
	s.mu.Lock()
	s.leases[lease.LeaseId] = lease
	s.mu.Unlock()
	resp := &daemonv1.AcquireStartPermitResponse{Granted: true, PermitLease: lease}
	remember(ctx, s, "AcquireStartPermit", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ReleaseStartPermit(ctx context.Context, req *daemonv1.ReleaseStartPermitRequest) (*daemonv1.ReleaseStartPermitResponse, error) {
	if resp, ok := replay(ctx, s, "ReleaseStartPermit", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.ReleaseStartPermitResponse {
		return &daemonv1.ReleaseStartPermitResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "ReleaseStartPermit", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.leases, req.GetLeaseId())
	s.mu.Unlock()
	resp := &daemonv1.ReleaseStartPermitResponse{Accepted: true}
	remember(ctx, s, "ReleaseStartPermit", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListInteractionEndpoints(ctx context.Context, req *daemonv1.ListInteractionEndpointsRequest) (*daemonv1.ListInteractionEndpointsResponse, error) {
	endpoints, err := s.store.ListInteractionEndpoints(ctx, int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list interaction endpoints: %v", err)
	}
	out := make([]*daemonv1.InteractionEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, endpointToProto(endpoint))
	}
	return &daemonv1.ListInteractionEndpointsResponse{
		Endpoints:  out,
		NextCursor: cursorFromCount(len(out), "", s.serverID),
	}, nil
}

func (s *Server) GetServerInfo(context.Context, *daemonv1.GetServerInfoRequest) (*daemonv1.GetServerInfoResponse, error) {
	return &daemonv1.GetServerInfoResponse{
		ServerName:         s.serverName,
		Version:            version.Version,
		ProtocolVersion:    protocolVersion,
		MinProtocolVersion: protocolVersion,
		MaxProtocolVersion: protocolVersion,
		ServerId:           s.serverID,
	}, nil
}

func (s *Server) ListChannels(ctx context.Context, req *daemonv1.ListChannelsRequest) (*daemonv1.ListChannelsResponse, error) {
	tasks, err := s.store.ListTasks(ctx, "", "", 200)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list task channels: %v", err)
	}
	messages, err := s.store.ListMessages(ctx, "#general", int(req.GetLimit()))
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "list messages: %v", err)
	}
	seen := map[string]bool{"#general": true}
	channels := []*daemonv1.ChannelRecord{channelRecord("#general", "General")}
	for _, taskModel := range tasks {
		if taskModel.Target == "" || seen[taskModel.Target] {
			continue
		}
		seen[taskModel.Target] = true
		channels = append(channels, channelRecord(taskModel.Target, strings.TrimPrefix(taskModel.Target, "#")))
	}
	for _, msg := range messages {
		if msg.Target == "" || seen[msg.Target] {
			continue
		}
		seen[msg.Target] = true
		channels = append(channels, channelRecord(msg.Target, strings.TrimPrefix(msg.Target, "#")))
	}
	return &daemonv1.ListChannelsResponse{Channels: channels, NextCursor: cursorFromCount(len(channels), "", s.serverID)}, nil
}

func (s *Server) SendMessage(ctx context.Context, req *daemonv1.SendMessageRequest) (*daemonv1.SendMessageResponse, error) {
	if resp, ok := replay(ctx, s, "SendMessage", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.SendMessageResponse {
		return &daemonv1.SendMessageResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "SendMessage", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetTarget() == "" || req.GetContent() == "" {
		return nil, status.Error(codes.InvalidArgument, "target and content are required")
	}
	role := req.GetRole()
	if role == "" {
		role = "user"
	}
	sender := req.GetSender()
	messageModel := storage.Message{
		Target:            req.GetTarget(),
		Role:              role,
		Content:           req.GetContent(),
		SenderUserID:      sender.GetUserId(),
		SenderAgentID:     sender.GetAgentId(),
		SenderDisplayName: sender.GetDisplayName(),
		SenderKind:        actorKindToStorage(sender.GetActorKind()),
		SourceEndpointID:  req.GetSourceEndpointId(),
		MetadataJSON:      normalizedJSON(req.GetMetadataJson()),
		RequestID:         firstNonEmpty(req.GetIdempotencyKey(), req.GetRequestId()),
	}
	if messageModel.SenderKind == "" {
		messageModel.SenderKind = "agent"
	}
	created, err := s.store.CreateMessage(ctx, messageModel)
	if errors.Is(err, storage.ErrConflict) {
		return nil, status.Error(codes.AlreadyExists, "message request already exists")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create message: %v", err)
	}
	if err := s.RecordMessageMutation(ctx, created, daemonv1.EventOperation_EVENT_OPERATION_APPENDED); err != nil {
		return nil, status.Errorf(codes.Internal, "append message event: %v", err)
	}
	msg := messageToProto(created)
	resp := &daemonv1.SendMessageResponse{Accepted: true, Message: msg}
	remember(ctx, s, "SendMessage", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ReadMessages(ctx context.Context, req *daemonv1.ReadMessagesRequest) (*daemonv1.ReadMessagesResponse, error) {
	if req.GetTarget() == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}
	messages, err := s.store.ListMessages(ctx, req.GetTarget(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list messages: %v", err)
	}
	out := make([]*daemonv1.CollaborationMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, messageToProto(msg))
	}
	return &daemonv1.ReadMessagesResponse{Messages: out, NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) CreateCollaborationTask(ctx context.Context, req *daemonv1.CreateCollaborationTaskRequest) (*daemonv1.CreateCollaborationTaskResponse, error) {
	if resp, ok := replay(ctx, s, "CreateCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.CreateCollaborationTaskResponse {
		return &daemonv1.CreateCollaborationTaskResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "CreateCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetTarget() == "" || req.GetSummary() == "" {
		return nil, status.Error(codes.InvalidArgument, "target and summary are required")
	}
	created, err := s.store.CreateTask(ctx, storage.Task{
		Summary:         req.GetSummary(),
		State:           "todo",
		Target:          req.GetTarget(),
		AssigneeID:      req.GetAgentId(),
		CreatedByUserID: req.GetCreatedByUserId(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create task: %v", err)
	}
	if err := s.RecordTaskMutation(ctx, created, daemonv1.EventOperation_EVENT_OPERATION_CREATED); err != nil {
		return nil, status.Errorf(codes.Internal, "append task event: %v", err)
	}
	resp := &daemonv1.CreateCollaborationTaskResponse{Task: taskToProto(created)}
	remember(ctx, s, "CreateCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) GetTask(ctx context.Context, req *daemonv1.GetTaskRequest) (*daemonv1.GetTaskResponse, error) {
	taskModel, err := s.store.GetTask(ctx, req.GetTaskId())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get task: %v", err)
	}
	return &daemonv1.GetTaskResponse{Task: taskToProto(taskModel)}, nil
}

func (s *Server) UpdateTask(ctx context.Context, req *daemonv1.UpdateTaskRequest) (*daemonv1.UpdateTaskResponse, error) {
	if resp, ok := replay(ctx, s, "UpdateTask", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.UpdateTaskResponse {
		return &daemonv1.UpdateTaskResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "UpdateTask", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	patch := storage.TaskPatch{}
	if req.Summary != nil {
		value := req.GetSummary()
		patch.Summary = &value
	}
	if req.State != nil {
		value, ok := taskStateToStorage(req.GetState())
		if !ok {
			return &daemonv1.UpdateTaskResponse{Accepted: false, RejectionReason: "invalid_state"}, nil
		}
		patch.State = &value
	}
	taskModel, err := s.store.UpdateTask(ctx, req.GetTaskId(), patch)
	if errors.Is(err, storage.ErrNotFound) {
		return &daemonv1.UpdateTaskResponse{Accepted: false, RejectionReason: "task_not_found"}, nil
	}
	if errors.Is(err, storage.ErrInvalidState) {
		return &daemonv1.UpdateTaskResponse{Accepted: false, RejectionReason: "invalid_state"}, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update task: %v", err)
	}
	operation := daemonv1.EventOperation_EVENT_OPERATION_UPDATED
	if req.State != nil {
		operation = daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED
	}
	if err := s.RecordTaskMutation(ctx, taskModel, operation); err != nil {
		return nil, status.Errorf(codes.Internal, "append task event: %v", err)
	}
	resp := &daemonv1.UpdateTaskResponse{Accepted: true, Task: taskToProto(taskModel)}
	remember(ctx, s, "UpdateTask", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListCollaborationTasks(ctx context.Context, req *daemonv1.ListCollaborationTasksRequest) (*daemonv1.ListCollaborationTasksResponse, error) {
	state := ""
	stateSet := make(map[string]struct{})
	if states := req.GetStates(); len(states) == 1 {
		if value, ok := taskStateToStorage(states[0]); ok {
			state = value
			stateSet[value] = struct{}{}
		}
	} else {
		for _, protoState := range states {
			if value, ok := taskStateToStorage(protoState); ok {
				stateSet[value] = struct{}{}
			}
		}
	}
	tasks, err := s.store.ListTasks(ctx, state, req.GetTarget(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tasks: %v", err)
	}
	out := make([]*daemonv1.Task, 0, len(tasks))
	for _, taskModel := range tasks {
		if len(stateSet) > 0 {
			if _, ok := stateSet[taskModel.State]; !ok {
				continue
			}
		}
		if req.GetAgentId() != "" && taskModel.AssigneeID != req.GetAgentId() {
			continue
		}
		out = append(out, taskToProto(taskModel))
	}
	return &daemonv1.ListCollaborationTasksResponse{Tasks: out, NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) ListTaskBoard(ctx context.Context, req *daemonv1.ListTaskBoardRequest) (*daemonv1.ListTaskBoardResponse, error) {
	tasks, err := s.store.ListTasks(ctx, "", req.GetTarget(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list task board: %v", err)
	}
	byColumn := map[string][]*daemonv1.Task{}
	for _, taskModel := range tasks {
		if req.GetAssigneeId() != "" && taskModel.AssigneeID != req.GetAssigneeId() {
			continue
		}
		protoTask := taskToProto(taskModel)
		column := protoTask.GetBoardColumn()
		if req.GetColumn() != "" && column != req.GetColumn() {
			continue
		}
		byColumn[column] = append(byColumn[column], protoTask)
	}
	columns := make([]*daemonv1.TaskBoardColumn, 0, len(byColumn))
	counts := make(map[string]int64, len(byColumn))
	for _, column := range []string{"todo", "in_progress", "blocked", "in_review", "done", "canceled"} {
		items := byColumn[column]
		if len(items) == 0 {
			continue
		}
		counts[column] = int64(len(items))
		columns = append(columns, &daemonv1.TaskBoardColumn{Column: column, Tasks: items, TotalCount: int64(len(items))})
	}
	return &daemonv1.ListTaskBoardResponse{Board: &daemonv1.TaskBoardSnapshot{
		Columns:      columns,
		ColumnCounts: counts,
		NextCursor:   cursorFromCount(len(tasks), req.GetTarget(), s.serverID),
	}}, nil
}

func (s *Server) ClaimCollaborationTask(ctx context.Context, req *daemonv1.ClaimCollaborationTaskRequest) (*daemonv1.ClaimCollaborationTaskResponse, error) {
	if resp, ok := replay(ctx, s, "ClaimCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.ClaimCollaborationTaskResponse {
		return &daemonv1.ClaimCollaborationTaskResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "ClaimCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	lease := newLease("task_claim", req.GetTaskId(), req.GetAgentId(), int64(defaultTTL(req.GetLeaseTtlSeconds(), 300)))
	updated, accepted, err := s.store.ClaimTaskCAS(ctx, req.GetTaskId(), req.GetAgentId(), lease.GetLeaseId())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "claim task: %v", err)
	}
	if !accepted {
		resp := &daemonv1.ClaimCollaborationTaskResponse{
			Task:              taskToProto(updated),
			Accepted:          false,
			ConflictReason:    "already_claimed",
			CurrentAssigneeId: updated.AssigneeID,
			ConflictPolicy:    daemonv1.TaskClaimConflictBehavior_TASK_CLAIM_CONFLICT_BEHAVIOR_FAIL_SILENT,
		}
		remember(ctx, s, "ClaimCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey(), resp)
		return resp, nil
	}
	s.mu.Lock()
	s.leases[lease.LeaseId] = lease
	s.mu.Unlock()
	if err := s.RecordTaskMutation(ctx, updated, daemonv1.EventOperation_EVENT_OPERATION_CLAIMED); err != nil {
		return nil, status.Errorf(codes.Internal, "append task claim event: %v", err)
	}
	resp := &daemonv1.ClaimCollaborationTaskResponse{Task: taskToProto(updated), Accepted: true, ClaimLease: lease}
	remember(ctx, s, "ClaimCollaborationTask", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) UpdateAgentStatus(ctx context.Context, req *daemonv1.UpdateAgentStatusRequest) (*daemonv1.UpdateAgentStatusResponse, error) {
	if resp, ok := replay(ctx, s, "UpdateAgentStatus", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.UpdateAgentStatusResponse {
		return &daemonv1.UpdateAgentStatusResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "UpdateAgentStatus", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetStatus().GetAgentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	cp := proto.Clone(req.GetStatus()).(*daemonv1.AgentStatusSnapshot)
	if cp.UpdatedTimeUnix == 0 {
		cp.UpdatedTimeUnix = unixNow()
	}
	s.mu.Lock()
	s.statuses[cp.AgentId] = cp
	s.mu.Unlock()
	resp := &daemonv1.UpdateAgentStatusResponse{Status: cp}
	remember(ctx, s, "UpdateAgentStatus", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListAgentStatuses(_ context.Context, req *daemonv1.ListAgentStatusesRequest) (*daemonv1.ListAgentStatusesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	statuses := make([]*daemonv1.AgentStatusSnapshot, 0, len(s.statuses))
	for _, snapshot := range s.statuses {
		if req.GetAgentId() != "" && snapshot.GetAgentId() != req.GetAgentId() {
			continue
		}
		if req.GetTarget() != "" && snapshot.GetTarget() != req.GetTarget() {
			continue
		}
		statuses = append(statuses, proto.Clone(snapshot).(*daemonv1.AgentStatusSnapshot))
		if len(statuses) >= limit {
			break
		}
	}
	return &daemonv1.ListAgentStatusesResponse{Statuses: statuses, NextCursor: cursorFromCount(len(statuses), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) ListAgentProfiles(_ context.Context, req *daemonv1.ListAgentProfilesRequest) (*daemonv1.ListAgentProfilesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	profiles := make([]*daemonv1.AgentProfile, 0, limit)
	for _, snapshot := range s.statuses {
		profiles = append(profiles, agentProfileFromStatus(snapshot))
		if len(profiles) >= limit {
			break
		}
	}
	return &daemonv1.ListAgentProfilesResponse{Profiles: profiles, NextCursor: cursorFromCount(len(profiles), "", s.serverID)}, nil
}

func (s *Server) AcknowledgeServerEvents(_ context.Context, req *daemonv1.AcknowledgeServerEventsRequest) (*daemonv1.AcknowledgeServerEventsResponse, error) {
	return &daemonv1.AcknowledgeServerEventsResponse{Accepted: true, Cursor: req.GetCursor()}, nil
}

func (s *Server) SubscribeServerEvents(req *daemonv1.SubscribeServerEventsRequest, stream daemonv1.DaemonControlService_SubscribeServerEventsServer) error {
	event := &daemonv1.ServerEvent{
		EventId:         storage.NewID("evt"),
		Sequence:        s.nextSequence(),
		AggregateId:     req.GetComputerId(),
		Target:          req.GetComputerId(),
		Kind:            daemonv1.ServerEventKind_SERVER_EVENT_KIND_PING,
		CreatedTimeUnix: unixNow(),
		ProtocolVersion: protocolVersion,
		RequestId:       req.GetRequestId(),
		Operation:       daemonv1.EventOperation_EVENT_OPERATION_HEARTBEAT,
		Scope: &daemonv1.EventScope{
			ScopeType: daemonv1.EventScopeType_EVENT_SCOPE_TYPE_COMPUTER,
			ScopeId:   req.GetComputerId(),
		},
		Payload: &daemonv1.ServerEvent_Ping{Ping: &daemonv1.ServerPing{
			ServerTimeUnix: unixNow(),
			TimeoutSeconds: 60,
		}},
	}
	if err := stream.Send(&daemonv1.SubscribeServerEventsResponse{Event: event}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func (s *Server) LogActivity(ctx context.Context, req *daemonv1.LogActivityRequest) (*daemonv1.LogActivityResponse, error) {
	if resp, ok := replay(ctx, s, "LogActivity", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.LogActivityResponse {
		return &daemonv1.LogActivityResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "LogActivity", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetTarget() == "" || req.GetKind() == "" {
		return nil, status.Error(codes.InvalidArgument, "target and kind are required")
	}
	activity := &daemonv1.ActivityRecord{
		ActivityId:      storage.NewID("act"),
		Target:          req.GetTarget(),
		AgentId:         req.GetAgentId(),
		Kind:            req.GetKind(),
		Summary:         req.GetSummary(),
		Detail:          req.GetDetail(),
		CreatedTimeUnix: unixNow(),
		RunId:           req.GetRunId(),
		StepId:          req.GetStepId(),
		AggregateId:     req.GetTarget(),
		ProtocolVersion: protocolVersion,
	}
	payload, err := protojson.Marshal(activity)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal activity: %v", err)
	}
	event, err := s.store.AppendCollaborationEvent(ctx, storage.CollaborationEvent{
		ServerID:        s.serverID,
		Target:          activity.GetTarget(),
		AggregateID:     activity.GetAggregateId(),
		Kind:            "activity",
		ActivityID:      activity.GetActivityId(),
		Operation:       eventOperationToStorage(daemonv1.EventOperation_EVENT_OPERATION_CREATED),
		ScopeType:       eventScopeTypeToStorage(daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET),
		ScopeID:         activity.GetTarget(),
		PayloadJSON:     string(payload),
		CreatedUnix:     activity.GetCreatedTimeUnix(),
		ProtocolVersion: int(protocolVersion),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "append collaboration event: %v", err)
	}
	activity.Sequence = event.Sequence
	activity.ProtocolVersion = int32(event.ProtocolVersion)
	resp := &daemonv1.LogActivityResponse{Activity: proto.Clone(activity).(*daemonv1.ActivityRecord)}
	remember(ctx, s, "LogActivity", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListActivity(ctx context.Context, req *daemonv1.ListActivityRequest) (*daemonv1.ListActivityResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	events, err := s.store.ListRecentCollaborationEvents(ctx, s.serverID, req.GetTarget(), "activity", 200)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list collaboration events: %v", err)
	}
	out := make([]*daemonv1.ActivityRecord, 0, limit)
	for _, event := range events {
		if len(out) >= limit {
			break
		}
		activity, ok := activityFromEvent(event)
		if !ok {
			continue
		}
		if req.GetAgentId() != "" && activity.GetAgentId() != req.GetAgentId() {
			continue
		}
		out = append(out, activity)
	}
	return &daemonv1.ListActivityResponse{Activities: out, NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) AcknowledgeActivityEvents(_ context.Context, req *daemonv1.AcknowledgeActivityEventsRequest) (*daemonv1.AcknowledgeActivityEventsResponse, error) {
	return &daemonv1.AcknowledgeActivityEventsResponse{Accepted: true, Cursor: req.GetCursor()}, nil
}

func (s *Server) SubscribeActivity(req *daemonv1.SubscribeActivityRequest, stream daemonv1.DaemonControlService_SubscribeActivityServer) error {
	event := &daemonv1.CollaborationEvent{
		EventId:         storage.NewID("cev"),
		Target:          firstNonEmpty(first(req.GetTargets()), "server"),
		Kind:            daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_PING,
		CreatedTimeUnix: unixNow(),
		Sequence:        s.nextSequence(),
		AggregateId:     firstNonEmpty(first(req.GetTargets()), "server"),
		ProtocolVersion: protocolVersion,
		Operation:       daemonv1.EventOperation_EVENT_OPERATION_HEARTBEAT,
		Scope: &daemonv1.EventScope{
			ScopeType: daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET,
			ScopeId:   firstNonEmpty(first(req.GetTargets()), "server"),
			Target:    firstNonEmpty(first(req.GetTargets()), "server"),
		},
	}
	if err := stream.Send(&daemonv1.SubscribeActivityResponse{Event: event}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func (s *Server) UpdateRunStatus(ctx context.Context, req *daemonv1.UpdateRunStatusRequest) (*daemonv1.UpdateRunStatusResponse, error) {
	if resp, ok := replay(ctx, s, "UpdateRunStatus", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.UpdateRunStatusResponse {
		return &daemonv1.UpdateRunStatusResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "UpdateRunStatus", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	now := unixNow()
	s.mu.Lock()
	run := s.runs[req.GetRunId()]
	if run == nil {
		run = &daemonv1.Run{RunId: req.GetRunId(), AgentId: req.GetAgentId(), LeaseId: req.GetLeaseId(), StartedTimeUnix: now}
		s.runs[req.GetRunId()] = run
	}
	run.State = req.GetState()
	run.Summary = req.GetSummary()
	run.Error = req.GetError()
	run.UpdatedTimeUnix = now
	if isTerminalRunState(run.State) {
		run.CompletedTimeUnix = now
	}
	cp := proto.Clone(run).(*daemonv1.Run)
	s.mu.Unlock()
	resp := &daemonv1.UpdateRunStatusResponse{Accepted: true, Run: cp}
	remember(ctx, s, "UpdateRunStatus", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) RenewRunLease(ctx context.Context, req *daemonv1.RenewRunLeaseRequest) (*daemonv1.RenewRunLeaseResponse, error) {
	if resp, ok := replay(ctx, s, "RenewRunLease", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.RenewRunLeaseResponse {
		return &daemonv1.RenewRunLeaseResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "RenewRunLease", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetRunId() == "" || req.GetLeaseId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id and lease_id are required")
	}
	ttl := int64(defaultTTL(req.GetLeaseTtlSeconds(), 300))
	s.mu.Lock()
	lease := s.leases[req.GetLeaseId()]
	if lease == nil {
		s.mu.Unlock()
		return &daemonv1.RenewRunLeaseResponse{Accepted: false, RejectionReason: "lease_not_found"}, nil
	}
	lease.ExpiresTimeUnix = unixNow() + ttl
	run := s.runs[req.GetRunId()]
	var runCopy *daemonv1.Run
	if run != nil {
		runCopy = proto.Clone(run).(*daemonv1.Run)
	}
	leaseCopy := cloneLease(lease)
	s.mu.Unlock()
	resp := &daemonv1.RenewRunLeaseResponse{Accepted: true, Run: runCopy, Lease: leaseCopy}
	remember(ctx, s, "RenewRunLease", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) AppendRunStep(ctx context.Context, req *daemonv1.AppendRunStepRequest) (*daemonv1.AppendRunStepResponse, error) {
	if resp, ok := replay(ctx, s, "AppendRunStep", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.AppendRunStepResponse {
		return &daemonv1.AppendRunStepResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "AppendRunStep", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	step := req.GetStep()
	if step.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	cp := proto.Clone(step).(*daemonv1.RunStep)
	if cp.StepId == "" {
		cp.StepId = storage.NewID("step")
	}
	s.mu.Lock()
	s.runSteps[cp.RunId] = append(s.runSteps[cp.RunId], cp)
	s.mu.Unlock()
	resp := &daemonv1.AppendRunStepResponse{Accepted: true, Step: proto.Clone(cp).(*daemonv1.RunStep)}
	remember(ctx, s, "AppendRunStep", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) FetchAssignedRuns(_ context.Context, req *daemonv1.FetchAssignedRunsRequest) (*daemonv1.FetchAssignedRunsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	runs := make([]*daemonv1.Run, 0, limit)
	for _, run := range s.runs {
		if req.GetComputerId() != "" && run.GetComputerId() != req.GetComputerId() {
			continue
		}
		if len(req.GetAgentIds()) > 0 && !contains(req.GetAgentIds(), run.GetAgentId()) {
			continue
		}
		runs = append(runs, proto.Clone(run).(*daemonv1.Run))
		if len(runs) >= limit {
			break
		}
	}
	return &daemonv1.FetchAssignedRunsResponse{Runs: runs, NextCursor: cursorFromCount(len(runs), req.GetComputerId(), s.serverID)}, nil
}

func (s *Server) ListRuns(_ context.Context, req *daemonv1.ListRunsRequest) (*daemonv1.ListRunsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	runs := make([]*daemonv1.Run, 0, limit)
	for _, run := range s.runs {
		if req.GetTarget() != "" && run.GetTarget() != req.GetTarget() {
			continue
		}
		if req.GetTaskId() != "" && run.GetTaskId() != req.GetTaskId() {
			continue
		}
		if req.GetAgentId() != "" && run.GetAgentId() != req.GetAgentId() {
			continue
		}
		runs = append(runs, proto.Clone(run).(*daemonv1.Run))
		if len(runs) >= limit {
			break
		}
	}
	return &daemonv1.ListRunsResponse{Runs: runs, NextCursor: cursorFromCount(len(runs), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) GetRun(_ context.Context, req *daemonv1.GetRunRequest) (*daemonv1.GetRunResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[req.GetRunId()]
	if run == nil {
		return nil, status.Error(codes.NotFound, "run not found")
	}
	steps := make([]*daemonv1.RunStep, 0, len(s.runSteps[req.GetRunId()]))
	for _, step := range s.runSteps[req.GetRunId()] {
		steps = append(steps, proto.Clone(step).(*daemonv1.RunStep))
	}
	return &daemonv1.GetRunResponse{Run: proto.Clone(run).(*daemonv1.Run), Steps: steps}, nil
}

func (s *Server) ListEventsSince(ctx context.Context, req *daemonv1.ListEventsSinceRequest) (*daemonv1.ListEventsSinceResponse, error) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	cursor := req.GetCursor()
	fromSequence := cursor.GetSequence()
	target := cursor.GetTarget()
	aggregateID := cursor.GetAggregateId()
	stored, err := s.store.ListCollaborationEvents(ctx, s.serverID, target, aggregateID, fromSequence, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list collaboration events: %v", err)
	}
	events := make([]*daemonv1.CollaborationEvent, 0, len(stored))
	lastSequence := fromSequence
	for _, event := range stored {
		events = append(events, collaborationEventToProto(event))
		lastSequence = event.Sequence
	}
	return &daemonv1.ListEventsSinceResponse{
		Events:     events,
		NextCursor: cursorFromSequence(lastSequence, firstNonEmpty(target, aggregateID), s.serverID),
	}, nil
}

func (s *Server) nextSequence() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventSeq++
	return s.eventSeq
}

func replay[T proto.Message](ctx context.Context, s *Server, method, requestID, idempotencyKey string, newMessage func() T) (T, bool) {
	var zero T
	key := idempotencyCacheKey(method, requestID, idempotencyKey)
	if key == "" {
		return zero, false
	}
	s.mu.Lock()
	msg, ok := s.idempotency[key]
	s.mu.Unlock()
	if ok {
		typed, ok := proto.Clone(msg).(T)
		return typed, ok
	}
	if s.store == nil {
		return zero, false
	}
	record, err := s.store.GetIdempotencyRecord(ctx, "daemonrpc", method, "", firstNonEmpty(idempotencyKey, requestID))
	if err != nil || record.Status != "completed" || record.ResponseJSON == "" {
		return zero, false
	}
	resp := newMessage()
	if err := protojson.Unmarshal([]byte(record.ResponseJSON), resp); err != nil {
		return zero, false
	}
	return resp, true
}

func reserve(ctx context.Context, s *Server, method, requestID, idempotencyKey string) error {
	if s.store == nil {
		return nil
	}
	key := firstNonEmpty(idempotencyKey, requestID)
	if key == "" {
		return nil
	}
	record, created, err := s.store.ReserveIdempotencyRecord(ctx, storage.IdempotencyRecord{
		Scope:          "daemonrpc",
		Method:         method,
		IdempotencyKey: key,
		Status:         "pending",
	})
	if err != nil {
		return status.Errorf(codes.Internal, "reserve idempotency: %v", err)
	}
	if created || record.Status == "completed" {
		return nil
	}
	return status.Error(codes.Aborted, "idempotency key is already in progress")
}

func remember(ctx context.Context, s *Server, method, requestID, idempotencyKey string, msg proto.Message) {
	key := idempotencyCacheKey(method, requestID, idempotencyKey)
	if key == "" || msg == nil {
		return
	}
	s.mu.Lock()
	s.idempotency[key] = proto.Clone(msg)
	s.mu.Unlock()
	if s.store == nil {
		return
	}
	keyValue := firstNonEmpty(idempotencyKey, requestID)
	body, err := protojson.Marshal(msg)
	if err != nil {
		return
	}
	_ = s.store.CompleteIdempotencyRecord(ctx, storage.IdempotencyRecord{
		Scope:          "daemonrpc",
		Method:         method,
		IdempotencyKey: keyValue,
		ResponseType:   string(proto.MessageName(msg)),
		ResponseJSON:   string(body),
		Status:         "completed",
	})
}

func idempotencyCacheKey(method, requestID, idempotencyKey string) string {
	key := firstNonEmpty(idempotencyKey, requestID)
	if key == "" {
		return ""
	}
	return method + ":" + key
}

func (s *Server) RecordMessageMutation(ctx context.Context, msg storage.Message, operation daemonv1.EventOperation) error {
	if s == nil || s.store == nil {
		return nil
	}
	protoMsg := messageToProto(msg)
	payload, err := protojson.Marshal(protoMsg)
	if err != nil {
		return err
	}
	aggregateID := firstNonEmpty(protoMsg.GetAggregateId(), msg.Target)
	_, err = s.store.AppendCollaborationEvent(ctx, storage.CollaborationEvent{
		ServerID:        s.serverID,
		Target:          msg.Target,
		AggregateID:     aggregateID,
		Kind:            "message",
		Operation:       eventOperationToStorage(operation),
		ScopeType:       eventScopeTypeToStorage(daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET),
		ScopeID:         msg.Target,
		PayloadJSON:     string(payload),
		CreatedUnix:     msg.CreatedUnix,
		ProtocolVersion: int(protocolVersion),
	})
	return err
}

func (s *Server) RecordTaskMutation(ctx context.Context, task storage.Task, operation daemonv1.EventOperation) error {
	if s == nil || s.store == nil {
		return nil
	}
	protoTask := taskToProto(task)
	payload, err := protojson.Marshal(protoTask)
	if err != nil {
		return err
	}
	_, err = s.store.AppendCollaborationEvent(ctx, storage.CollaborationEvent{
		ServerID:        s.serverID,
		Target:          task.Target,
		AggregateID:     task.ID,
		Kind:            "task",
		Operation:       eventOperationToStorage(operation),
		ScopeType:       eventScopeTypeToStorage(daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TASK),
		ScopeID:         task.ID,
		PayloadJSON:     string(payload),
		CreatedUnix:     task.UpdatedUnix,
		ProtocolVersion: int(protocolVersion),
	})
	return err
}

func collaborationEventToProto(event storage.CollaborationEvent) *daemonv1.CollaborationEvent {
	operation := eventOperationFromStorage(event.Operation)
	if operation == daemonv1.EventOperation_EVENT_OPERATION_UNSPECIFIED {
		operation = defaultEventOperationForKind(event.Kind)
	}
	scopeType := eventScopeTypeFromStorage(event.ScopeType)
	if scopeType == daemonv1.EventScopeType_EVENT_SCOPE_TYPE_UNSPECIFIED {
		scopeType = daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET
	}
	scopeID := firstNonEmpty(event.ScopeID, event.Target, event.AggregateID)
	out := &daemonv1.CollaborationEvent{
		EventId:         event.EventID,
		Target:          event.Target,
		Kind:            collaborationEventKindFromStorage(event.Kind),
		ActivityId:      event.ActivityID,
		CreatedTimeUnix: event.CreatedUnix,
		Sequence:        event.Sequence,
		AggregateId:     event.AggregateID,
		ProtocolVersion: int32(event.ProtocolVersion),
		Operation:       operation,
		Scope: &daemonv1.EventScope{
			ScopeType: scopeType,
			ScopeId:   scopeID,
			Target:    event.Target,
		},
		WorkspaceId: event.WorkspaceID,
	}
	if activity, ok := activityFromEvent(event); ok {
		out.Payload = &daemonv1.CollaborationEvent_Activity{Activity: activity}
	} else if msg, ok := messageFromEvent(event); ok {
		out.MessageId = msg.GetMessageId()
		out.SourceEndpointId = msg.GetSourceEndpointId()
		out.Payload = &daemonv1.CollaborationEvent_Message{Message: msg}
	} else if task, ok := taskFromEvent(event); ok {
		out.TaskId = task.GetTaskId()
		out.Payload = &daemonv1.CollaborationEvent_Task{Task: task}
	}
	return out
}

func activityFromEvent(event storage.CollaborationEvent) (*daemonv1.ActivityRecord, bool) {
	if event.Kind != "activity" {
		return nil, false
	}
	activity := &daemonv1.ActivityRecord{}
	if err := protojson.Unmarshal([]byte(event.PayloadJSON), activity); err != nil {
		return nil, false
	}
	activity.ActivityId = firstNonEmpty(activity.GetActivityId(), event.ActivityID)
	activity.Target = firstNonEmpty(activity.GetTarget(), event.Target)
	activity.Sequence = event.Sequence
	activity.AggregateId = firstNonEmpty(activity.GetAggregateId(), event.AggregateID)
	activity.CreatedTimeUnix = event.CreatedUnix
	activity.ProtocolVersion = int32(event.ProtocolVersion)
	return activity, true
}

func messageFromEvent(event storage.CollaborationEvent) (*daemonv1.CollaborationMessage, bool) {
	if event.Kind != "message" {
		return nil, false
	}
	msg := &daemonv1.CollaborationMessage{}
	if err := protojson.Unmarshal([]byte(event.PayloadJSON), msg); err != nil {
		return nil, false
	}
	msg.Sequence = event.Sequence
	msg.AggregateId = firstNonEmpty(msg.GetAggregateId(), event.AggregateID, event.Target)
	return msg, true
}

func taskFromEvent(event storage.CollaborationEvent) (*daemonv1.Task, bool) {
	if event.Kind != "task" {
		return nil, false
	}
	task := &daemonv1.Task{}
	if err := protojson.Unmarshal([]byte(event.PayloadJSON), task); err != nil {
		return nil, false
	}
	return task, true
}

func collaborationEventKindFromStorage(kind string) daemonv1.CollaborationEventKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "activity":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_ACTIVITY
	case "message":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_MESSAGE
	case "task":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_TASK
	case "reminder":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_REMINDER
	case "coordination":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_COORDINATION
	case "memory":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_MEMORY
	case "run":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_RUN
	case "run_step":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_RUN_STEP
	case "attachment":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_ATTACHMENT
	case "ping":
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_PING
	default:
		return daemonv1.CollaborationEventKind_COLLABORATION_EVENT_KIND_UNSPECIFIED
	}
}

func defaultEventOperationForKind(kind string) daemonv1.EventOperation {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "message", "run_step":
		return daemonv1.EventOperation_EVENT_OPERATION_APPENDED
	case "ping":
		return daemonv1.EventOperation_EVENT_OPERATION_HEARTBEAT
	case "task", "run":
		return daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED
	default:
		return daemonv1.EventOperation_EVENT_OPERATION_CREATED
	}
}

func eventOperationFromStorage(kind string) daemonv1.EventOperation {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "appended", "message", "run_step":
		return daemonv1.EventOperation_EVENT_OPERATION_APPENDED
	case "heartbeat", "ping":
		return daemonv1.EventOperation_EVENT_OPERATION_HEARTBEAT
	case "state_changed", "task", "run":
		return daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED
	case "created":
		return daemonv1.EventOperation_EVENT_OPERATION_CREATED
	case "updated":
		return daemonv1.EventOperation_EVENT_OPERATION_UPDATED
	case "deleted":
		return daemonv1.EventOperation_EVENT_OPERATION_DELETED
	case "claimed":
		return daemonv1.EventOperation_EVENT_OPERATION_CLAIMED
	case "released":
		return daemonv1.EventOperation_EVENT_OPERATION_RELEASED
	case "failed":
		return daemonv1.EventOperation_EVENT_OPERATION_FAILED
	case "canceled":
		return daemonv1.EventOperation_EVENT_OPERATION_CANCELED
	case "invalidated":
		return daemonv1.EventOperation_EVENT_OPERATION_INVALIDATED
	case "snapshot":
		return daemonv1.EventOperation_EVENT_OPERATION_SNAPSHOT
	default:
		return daemonv1.EventOperation_EVENT_OPERATION_UNSPECIFIED
	}
}

func eventOperationToStorage(operation daemonv1.EventOperation) string {
	switch operation {
	case daemonv1.EventOperation_EVENT_OPERATION_CREATED:
		return "created"
	case daemonv1.EventOperation_EVENT_OPERATION_UPDATED:
		return "updated"
	case daemonv1.EventOperation_EVENT_OPERATION_DELETED:
		return "deleted"
	case daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED:
		return "state_changed"
	case daemonv1.EventOperation_EVENT_OPERATION_APPENDED:
		return "appended"
	case daemonv1.EventOperation_EVENT_OPERATION_CLAIMED:
		return "claimed"
	case daemonv1.EventOperation_EVENT_OPERATION_RELEASED:
		return "released"
	case daemonv1.EventOperation_EVENT_OPERATION_FAILED:
		return "failed"
	case daemonv1.EventOperation_EVENT_OPERATION_CANCELED:
		return "canceled"
	case daemonv1.EventOperation_EVENT_OPERATION_HEARTBEAT:
		return "heartbeat"
	case daemonv1.EventOperation_EVENT_OPERATION_INVALIDATED:
		return "invalidated"
	case daemonv1.EventOperation_EVENT_OPERATION_SNAPSHOT:
		return "snapshot"
	default:
		return ""
	}
}

func eventScopeTypeToStorage(scopeType daemonv1.EventScopeType) string {
	switch scopeType {
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_SERVER:
		return "server"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_WORKSPACE:
		return "workspace"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET:
		return "target"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_THREAD:
		return "thread"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TASK:
		return "task"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_RUN:
		return "run"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_AGENT:
		return "agent"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_COMPUTER:
		return "computer"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_USER:
		return "user"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_ENDPOINT:
		return "endpoint"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_DAEMON:
		return "daemon"
	case daemonv1.EventScopeType_EVENT_SCOPE_TYPE_CUSTOM:
		return "custom"
	default:
		return ""
	}
}

func eventScopeTypeFromStorage(scopeType string) daemonv1.EventScopeType {
	switch strings.ToLower(strings.TrimSpace(scopeType)) {
	case "server":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_SERVER
	case "workspace":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_WORKSPACE
	case "target":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET
	case "thread":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_THREAD
	case "task":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TASK
	case "run":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_RUN
	case "agent":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_AGENT
	case "computer":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_COMPUTER
	case "user":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_USER
	case "endpoint":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_ENDPOINT
	case "daemon":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_DAEMON
	case "custom":
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_CUSTOM
	default:
		return daemonv1.EventScopeType_EVENT_SCOPE_TYPE_UNSPECIFIED
	}
}

func messageToProto(msg storage.Message) *daemonv1.CollaborationMessage {
	return &daemonv1.CollaborationMessage{
		MessageId:         msg.ID,
		Target:            msg.Target,
		ThreadId:          msg.ThreadID,
		Role:              msg.Role,
		Content:           msg.Content,
		CreatedTimeUnix:   msg.CreatedUnix,
		RequestId:         msg.RequestID,
		SourceEndpointId:  msg.SourceEndpointID,
		ExternalMessageId: msg.ExternalMessageID,
		MetadataJson:      msg.MetadataJSON,
		Sender: &daemonv1.Actor{
			ActorKind:   actorKindFromStorage(msg.SenderKind),
			AgentId:     msg.SenderAgentID,
			UserId:      msg.SenderUserID,
			DisplayName: msg.SenderDisplayName,
		},
		AggregateId: msg.Target,
	}
}

func endpointToProto(endpoint storage.InteractionEndpoint) *daemonv1.InteractionEndpoint {
	return &daemonv1.InteractionEndpoint{
		EndpointId:      endpoint.ID,
		Kind:            endpoint.Kind,
		Provider:        endpoint.Provider,
		DisplayName:     endpoint.DisplayName,
		TargetPrefix:    endpoint.TargetPrefix,
		InboundEnabled:  endpoint.InboundEnabled,
		OutboundEnabled: endpoint.OutboundEnabled,
		AuthMode:        endpointAuthModeFromStorage(endpoint.AuthMode),
		ConfigJson:      endpoint.ConfigJSON,
	}
}

func channelRecord(target, displayName string) *daemonv1.ChannelRecord {
	if displayName == "" {
		displayName = target
	}
	return &daemonv1.ChannelRecord{
		Target:      target,
		ChannelId:   target,
		DisplayName: displayName,
		ChannelType: "channel",
		Enabled:     true,
		SourceKind:  "web",
	}
}

func agentProfileFromStatus(snapshot *daemonv1.AgentStatusSnapshot) *daemonv1.AgentProfile {
	if snapshot == nil {
		return nil
	}
	return &daemonv1.AgentProfile{
		AgentId:              snapshot.GetAgentId(),
		Name:                 snapshot.GetAgentId(),
		DisplayName:          snapshot.GetAgentId(),
		Enabled:              true,
		ComputerId:           snapshot.GetComputerId(),
		RuntimeProfileId:     snapshot.GetRuntimeProfileId(),
		Status:               snapshot.GetPresence(),
		LastActivityTimeUnix: snapshot.GetUpdatedTimeUnix(),
		StatusSnapshot:       proto.Clone(snapshot).(*daemonv1.AgentStatusSnapshot),
	}
}

func taskToProto(task storage.Task) *daemonv1.Task {
	return &daemonv1.Task{
		TaskId:          task.ID,
		Summary:         task.Summary,
		State:           taskStateFromStorage(task.State),
		Target:          task.Target,
		AssigneeId:      task.AssigneeID,
		CreatedByUserId: task.CreatedByUserID,
		BoardColumn:     task.State,
		CreatedTimeUnix: task.CreatedUnix,
		UpdatedTimeUnix: task.UpdatedUnix,
		ClaimPolicy:     daemonv1.TaskClaimPolicy_TASK_CLAIM_POLICY_EXCLUSIVE,
	}
}

func actorKindToStorage(kind daemonv1.ActorKind) string {
	switch kind {
	case daemonv1.ActorKind_ACTOR_KIND_HUMAN:
		return "human"
	case daemonv1.ActorKind_ACTOR_KIND_AGENT:
		return "agent"
	case daemonv1.ActorKind_ACTOR_KIND_DAEMON:
		return "daemon"
	case daemonv1.ActorKind_ACTOR_KIND_ENDPOINT:
		return "endpoint"
	case daemonv1.ActorKind_ACTOR_KIND_SYSTEM:
		return "system"
	default:
		return ""
	}
}

func actorKindFromStorage(kind string) daemonv1.ActorKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "human", "user":
		return daemonv1.ActorKind_ACTOR_KIND_HUMAN
	case "agent":
		return daemonv1.ActorKind_ACTOR_KIND_AGENT
	case "daemon":
		return daemonv1.ActorKind_ACTOR_KIND_DAEMON
	case "endpoint":
		return daemonv1.ActorKind_ACTOR_KIND_ENDPOINT
	case "system":
		return daemonv1.ActorKind_ACTOR_KIND_SYSTEM
	default:
		return daemonv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

func endpointAuthModeFromStorage(mode string) daemonv1.EndpointAuthMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "cookie":
		return daemonv1.EndpointAuthMode_ENDPOINT_AUTH_MODE_COOKIE
	case "bearer":
		return daemonv1.EndpointAuthMode_ENDPOINT_AUTH_MODE_BEARER
	case "webhook_signature":
		return daemonv1.EndpointAuthMode_ENDPOINT_AUTH_MODE_WEBHOOK_SIGNATURE
	case "mcp_token":
		return daemonv1.EndpointAuthMode_ENDPOINT_AUTH_MODE_MCP_TOKEN
	case "none":
		return daemonv1.EndpointAuthMode_ENDPOINT_AUTH_MODE_NONE
	default:
		return daemonv1.EndpointAuthMode_ENDPOINT_AUTH_MODE_UNSPECIFIED
	}
}

func taskStateToStorage(state daemonv1.TaskState) (string, bool) {
	switch state {
	case daemonv1.TaskState_TASK_STATE_TODO:
		return "todo", true
	case daemonv1.TaskState_TASK_STATE_IN_PROGRESS:
		return "in_progress", true
	case daemonv1.TaskState_TASK_STATE_IN_REVIEW:
		return "in_review", true
	case daemonv1.TaskState_TASK_STATE_DONE:
		return "done", true
	case daemonv1.TaskState_TASK_STATE_BLOCKED:
		return "blocked", true
	case daemonv1.TaskState_TASK_STATE_CANCELED:
		return "canceled", true
	default:
		return "", false
	}
}

func taskStateFromStorage(state string) daemonv1.TaskState {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "todo":
		return daemonv1.TaskState_TASK_STATE_TODO
	case "in_progress":
		return daemonv1.TaskState_TASK_STATE_IN_PROGRESS
	case "in_review":
		return daemonv1.TaskState_TASK_STATE_IN_REVIEW
	case "done":
		return daemonv1.TaskState_TASK_STATE_DONE
	case "blocked":
		return daemonv1.TaskState_TASK_STATE_BLOCKED
	case "canceled", "cancelled":
		return daemonv1.TaskState_TASK_STATE_CANCELED
	default:
		return daemonv1.TaskState_TASK_STATE_UNSPECIFIED
	}
}

func isTerminalRunState(state daemonv1.RunState) bool {
	switch state {
	case daemonv1.RunState_RUN_STATE_COMPLETED,
		daemonv1.RunState_RUN_STATE_FAILED,
		daemonv1.RunState_RUN_STATE_CANCELED:
		return true
	default:
		return false
	}
}

func newLease(resourceType, resourceID, holderID string, ttlSeconds int64) *daemonv1.Lease {
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	return &daemonv1.Lease{
		LeaseId:               storage.NewID("lease"),
		HolderId:              holderID,
		ResourceType:          resourceType,
		ResourceId:            resourceID,
		ExpiresTimeUnix:       unixNow() + ttlSeconds,
		HeartbeatAfterSeconds: 30,
	}
}

func cloneLease(lease *daemonv1.Lease) *daemonv1.Lease {
	if lease == nil {
		return nil
	}
	return proto.Clone(lease).(*daemonv1.Lease)
}

func cloneInventory(inventory *daemonv1.ComputerInventory) *daemonv1.ComputerInventory {
	if inventory == nil {
		return nil
	}
	return proto.Clone(inventory).(*daemonv1.ComputerInventory)
}

func cursorFromCount(count int, target, serverID string) *daemonv1.EventCursor {
	return &daemonv1.EventCursor{
		Cursor:          fmt.Sprintf("%s:%d", target, count),
		Target:          target,
		Sequence:        int64(count),
		AggregateId:     target,
		ProtocolVersion: protocolVersion,
		ServerId:        serverID,
	}
}

func cursorFromSequence(sequence int64, target, serverID string) *daemonv1.EventCursor {
	return &daemonv1.EventCursor{
		Cursor:          fmt.Sprintf("%s:%d", target, sequence),
		Target:          target,
		Sequence:        sequence,
		AggregateId:     target,
		ProtocolVersion: protocolVersion,
		ServerId:        serverID,
	}
}

func normalizedJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func defaultTTL(value uint32, fallback uint32) uint32 {
	if value == 0 {
		return fallback
	}
	return value
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func unixNow() int64 {
	return time.Now().Unix()
}

var _ daemonv1.DaemonControlServiceServer = (*Server)(nil)

func discardStream[T any](stream interface {
	Send(*T) error
	Context() context.Context
}) error {
	<-stream.Context().Done()
	if errors.Is(stream.Context().Err(), context.Canceled) {
		return nil
	}
	return stream.Context().Err()
}

var _ = io.EOF
