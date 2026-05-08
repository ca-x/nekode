package daemonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/runtimeadapter"
	"github.com/ca-x/nekode/internal/runtimecatalog"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/ca-x/nekode/internal/version"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	protocolVersion int32 = 1

	daemonHeartbeatAfterSeconds uint32 = 30
	computerLeaseSeconds        int64  = 90
	computerStaleAfterSeconds   int64  = int64(daemonHeartbeatAfterSeconds) * 2
	computerOfflineAfterSeconds int64  = computerLeaseSeconds
)

type Server struct {
	daemonv1.UnimplementedDaemonControlServiceServer

	store      *storage.Store
	serverID   string
	serverName string

	mu           sync.Mutex
	computers    map[string]*computerState
	leases       map[string]*daemonv1.Lease
	statuses     map[string]*daemonv1.AgentStatusSnapshot
	runs         map[string]*daemonv1.Run
	runSteps     map[string][]*daemonv1.RunStep
	controls     map[string]*daemonv1.AgentControlOperation
	serverEvents map[string]*daemonv1.ServerEvent
	activities   []*daemonv1.ActivityRecord
	eventSeq     int64
	idempotency  map[string]proto.Message
}

type computerState struct {
	info             *daemonv1.ComputerInfo
	inventory        *daemonv1.ComputerInventory
	inventoryVersion string
	lease            *daemonv1.Lease
	lastHeartbeat    int64
}

type ComputerInventorySnapshot struct {
	Info              *daemonv1.ComputerInfo      `json:"info,omitempty"`
	Inventory         *daemonv1.ComputerInventory `json:"inventory,omitempty"`
	InventoryVersion  string                      `json:"inventoryVersion,omitempty"`
	LastHeartbeatUnix int64                       `json:"lastHeartbeatUnix,omitempty"`
}

type CreateAgentInstanceInput struct {
	ComputerID  string            `json:"computerId"`
	RuntimeID   string            `json:"runtimeId"`
	TemplateID  string            `json:"templateId"`
	DisplayName string            `json:"displayName"`
	Name        string            `json:"name"`
	Target      string            `json:"target"`
	Options     map[string]string `json:"options"`
}

type CreateAgentInstanceResult struct {
	Agent          *daemonv1.AgentProfile   `json:"agent"`
	RuntimeProfile *daemonv1.RuntimeProfile `json:"runtimeProfile"`
}

func New(store *storage.Store, serverID string) *Server {
	if strings.TrimSpace(serverID) == "" {
		serverID = storage.NewID("srv")
	}
	return &Server{
		store:        store,
		serverID:     serverID,
		serverName:   "Nekode",
		computers:    make(map[string]*computerState),
		leases:       make(map[string]*daemonv1.Lease),
		statuses:     make(map[string]*daemonv1.AgentStatusSnapshot),
		runs:         make(map[string]*daemonv1.Run),
		runSteps:     make(map[string][]*daemonv1.RunStep),
		controls:     make(map[string]*daemonv1.AgentControlOperation),
		serverEvents: make(map[string]*daemonv1.ServerEvent),
		idempotency:  make(map[string]proto.Message),
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
	lease := newLease("computer", info.GetComputerId(), info.GetDaemonId(), computerLeaseSeconds)

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
		lease = newLease("computer", computerID, holder, computerLeaseSeconds)
		state.lease = lease
	}
	lease.ExpiresTimeUnix = now + computerLeaseSeconds
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
		NextHeartbeatAfterSeconds: daemonHeartbeatAfterSeconds,
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

func (s *Server) ListRuntimePresets(_ context.Context, req *daemonv1.ListRuntimePresetsRequest) (*daemonv1.ListRuntimePresetsResponse, error) {
	presets := runtimecatalog.List(req.GetIncludeExperimental(), req.GetKindPrefix(), req.GetLimit())
	return &daemonv1.ListRuntimePresetsResponse{
		Presets:    presets,
		NextCursor: cursorFromCount(len(presets), req.GetKindPrefix(), s.serverID),
	}, nil
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
	messages, err := s.store.ListMessages(ctx, "#general", "", int(req.GetLimit()))
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
		ThreadID:          req.GetReplyToMessageId(),
		Role:              role,
		Content:           req.GetContent(),
		ReplyToMessageID:  req.GetReplyToMessageId(),
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
	if shouldEnqueueSourceDelivery(req, messageModel.SenderKind) {
		if _, err := s.EnqueueSourceOutboundDelivery(ctx, created); err != nil {
			return nil, status.Errorf(codes.Internal, "enqueue outbound delivery: %v", err)
		}
	}
	msg := messageToProto(created)
	resp := &daemonv1.SendMessageResponse{Accepted: true, Message: msg}
	remember(ctx, s, "SendMessage", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListOutboundDeliveries(ctx context.Context, req *daemonv1.ListOutboundDeliveriesRequest) (*daemonv1.ListOutboundDeliveriesResponse, error) {
	statuses := make([]string, 0, len(req.GetStatuses()))
	for _, deliveryStatus := range req.GetStatuses() {
		if storageStatus := outboundDeliveryStatusToStorage(deliveryStatus); storageStatus != "" {
			statuses = append(statuses, storageStatus)
		}
	}
	deliveries, err := s.store.ListOutboundDeliveries(ctx, storage.OutboundDeliveryListOptions{
		Target:     req.GetTarget(),
		MessageID:  req.GetMessageId(),
		EndpointID: req.GetEndpointId(),
		Statuses:   statuses,
		Limit:      int(req.GetLimit()),
	})
	if errors.Is(err, storage.ErrInvalidState) {
		return nil, status.Error(codes.InvalidArgument, "invalid outbound delivery status")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list outbound deliveries: %v", err)
	}
	out := make([]*daemonv1.OutboundDeliveryRecord, 0, len(deliveries))
	for _, delivery := range deliveries {
		out = append(out, outboundDeliveryToProto(delivery))
	}
	return &daemonv1.ListOutboundDeliveriesResponse{
		Deliveries: out,
		NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID),
	}, nil
}

func (s *Server) RetryOutboundDelivery(ctx context.Context, req *daemonv1.RetryOutboundDeliveryRequest) (*daemonv1.RetryOutboundDeliveryResponse, error) {
	if resp, ok := replay(ctx, s, "RetryOutboundDelivery", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.RetryOutboundDeliveryResponse {
		return &daemonv1.RetryOutboundDeliveryResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "RetryOutboundDelivery", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetDeliveryId() == "" {
		return nil, status.Error(codes.InvalidArgument, "delivery_id is required")
	}
	delivery, err := s.store.ScheduleOutboundDeliveryRetry(ctx, req.GetDeliveryId(), unixNow())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "outbound delivery not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "retry outbound delivery: %v", err)
	}
	s.EmitOutboundDeliveryEvent(delivery, daemonv1.EventOperation_EVENT_OPERATION_UPDATED)
	resp := &daemonv1.RetryOutboundDeliveryResponse{Delivery: outboundDeliveryToProto(delivery)}
	remember(ctx, s, "RetryOutboundDelivery", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) RecordOutboundDeliveryStatus(ctx context.Context, deliveryID string, statusValue daemonv1.OutboundDeliveryStatus, lastError string, nextRetryUnix, deliveredUnix int64) (storage.OutboundDelivery, error) {
	storageStatus := outboundDeliveryStatusToStorage(statusValue)
	if storageStatus == "" {
		return storage.OutboundDelivery{}, storage.ErrInvalidState
	}
	if statusValue == daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED && deliveredUnix == 0 {
		deliveredUnix = unixNow()
	}
	if statusValue != daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_RETRYING {
		nextRetryUnix = 0
	}
	if statusValue != daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED {
		deliveredUnix = 0
	}
	delivery, err := s.store.UpdateOutboundDeliveryStatus(ctx, deliveryID, storageStatus, strings.TrimSpace(lastError), nextRetryUnix, deliveredUnix)
	if err != nil {
		return storage.OutboundDelivery{}, err
	}
	s.EmitOutboundDeliveryEvent(delivery, outboundDeliveryOperation(statusValue))
	return delivery, nil
}

func (s *Server) ReadMessages(ctx context.Context, req *daemonv1.ReadMessagesRequest) (*daemonv1.ReadMessagesResponse, error) {
	if req.GetTarget() == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}
	messages, err := s.store.ListMessages(ctx, req.GetTarget(), "", int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list messages: %v", err)
	}
	out := make([]*daemonv1.CollaborationMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, messageToProto(msg))
	}
	return &daemonv1.ReadMessagesResponse{Messages: out, NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) SearchMessages(ctx context.Context, req *daemonv1.SearchMessagesRequest) (*daemonv1.SearchMessagesResponse, error) {
	messages, err := s.store.SearchMessages(ctx, storage.MessageSearchOptions{
		Query:        req.GetQuery(),
		Target:       req.GetTarget(),
		SenderHandle: req.GetSenderHandle(),
		Sort:         messageSearchSortToStorage(req.GetSort()),
		Limit:        int(req.GetLimit()),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search messages: %v", err)
	}
	out := make([]*daemonv1.CollaborationMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, messageToProto(msg))
	}
	return &daemonv1.SearchMessagesResponse{Messages: out, NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) SaveMessage(ctx context.Context, req *daemonv1.SaveMessageRequest) (*daemonv1.SaveMessageResponse, error) {
	if resp, ok := replay(ctx, s, "SaveMessage", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.SaveMessageResponse {
		return &daemonv1.SaveMessageResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "SaveMessage", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetTarget() == "" || req.GetMessageId() == "" {
		return nil, status.Error(codes.InvalidArgument, "target and message_id are required")
	}
	saved, err := s.store.SaveMessage(ctx, req.GetTarget(), req.GetMessageId(), req.GetSavedByUserId(), req.GetSavedByAgentId())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "message not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "save message: %v", err)
	}
	resp := &daemonv1.SaveMessageResponse{Saved: true, SavedMessage: savedMessageToProto(saved)}
	remember(ctx, s, "SaveMessage", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) UnsaveMessage(ctx context.Context, req *daemonv1.UnsaveMessageRequest) (*daemonv1.UnsaveMessageResponse, error) {
	if resp, ok := replay(ctx, s, "UnsaveMessage", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.UnsaveMessageResponse {
		return &daemonv1.UnsaveMessageResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "UnsaveMessage", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetTarget() == "" || req.GetMessageId() == "" {
		return nil, status.Error(codes.InvalidArgument, "target and message_id are required")
	}
	saved, err := s.store.UnsaveMessage(ctx, req.GetTarget(), req.GetMessageId(), req.GetSavedByUserId(), req.GetSavedByAgentId())
	if errors.Is(err, storage.ErrNotFound) {
		return &daemonv1.UnsaveMessageResponse{Removed: false}, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unsave message: %v", err)
	}
	resp := &daemonv1.UnsaveMessageResponse{Removed: true, SavedMessage: savedMessageToProto(saved)}
	remember(ctx, s, "UnsaveMessage", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListSavedMessages(ctx context.Context, req *daemonv1.ListSavedMessagesRequest) (*daemonv1.ListSavedMessagesResponse, error) {
	saved, err := s.store.ListSavedMessages(ctx, req.GetTarget(), req.GetSavedByUserId(), req.GetSavedByAgentId(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list saved messages: %v", err)
	}
	out := make([]*daemonv1.SavedMessageRecord, 0, len(saved))
	for _, record := range saved {
		out = append(out, savedMessageToProto(record))
	}
	return &daemonv1.ListSavedMessagesResponse{SavedMessages: out, NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID)}, nil
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

func (s *Server) ScheduleReminder(ctx context.Context, req *daemonv1.ScheduleReminderRequest) (*daemonv1.ScheduleReminderResponse, error) {
	if resp, ok := replay(ctx, s, "ScheduleReminder", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.ScheduleReminderResponse {
		return &daemonv1.ScheduleReminderResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "ScheduleReminder", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	target := firstNonEmpty(req.GetTarget(), req.GetChannel())
	if target == "" {
		return nil, status.Error(codes.InvalidArgument, "target is required")
	}
	plan, err := reminderPlanFromScheduleRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actorType, actorID := reminderActorFromContext(req.GetContext())
	title := firstNonEmpty(req.GetTitle(), req.GetName(), req.GetPrompt())
	created, err := s.store.CreateReminder(ctx, storage.Reminder{
		Target:                target,
		ScheduleKind:          plan.Kind,
		Schedule:              plan.Schedule,
		Prompt:                req.GetPrompt(),
		Enabled:               true,
		NextRunUnix:           plan.NextRunUnix,
		Title:                 title,
		Status:                "active",
		MsgRef:                req.GetMsgId(),
		RecurrenceRule:        plan.RecurrenceRule,
		RecurrenceDescription: plan.RecurrenceDescription,
		RecurrenceTimezone:    plan.RecurrenceTimezone,
		CancelToken:           storage.NewID("rtk"),
	}, actorType, actorID, "created")
	if errors.Is(err, storage.ErrInvalidState) {
		return nil, status.Error(codes.InvalidArgument, "invalid reminder schedule")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create reminder: %v", err)
	}
	if err := s.RecordReminderMutation(ctx, created, daemonv1.EventOperation_EVENT_OPERATION_CREATED); err != nil {
		return nil, status.Errorf(codes.Internal, "append reminder event: %v", err)
	}
	resp := &daemonv1.ScheduleReminderResponse{Reminder: reminderToProto(created)}
	remember(ctx, s, "ScheduleReminder", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListReminders(ctx context.Context, req *daemonv1.ListRemindersRequest) (*daemonv1.ListRemindersResponse, error) {
	statuses := make([]string, 0, len(req.GetStatuses()))
	for _, statusValue := range req.GetStatuses() {
		if statusValue == daemonv1.ReminderStatus_REMINDER_STATUS_UNSPECIFIED {
			continue
		}
		statuses = append(statuses, reminderStatusToStorage(statusValue))
	}
	reminders, err := s.store.ListReminders(ctx, req.GetTarget(), statuses, req.GetIncludeCanceled(), int(req.GetLimit()))
	if errors.Is(err, storage.ErrInvalidState) {
		return nil, status.Error(codes.InvalidArgument, "invalid reminder status")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list reminders: %v", err)
	}
	out := make([]*daemonv1.ReminderRecord, 0, len(reminders))
	for _, reminderModel := range reminders {
		out = append(out, reminderToProto(reminderModel))
	}
	return &daemonv1.ListRemindersResponse{
		Reminders:  out,
		NextCursor: cursorFromCount(len(out), req.GetTarget(), s.serverID),
	}, nil
}

func (s *Server) CancelReminder(ctx context.Context, req *daemonv1.CancelReminderRequest) (*daemonv1.CancelReminderResponse, error) {
	if resp, ok := replay(ctx, s, "CancelReminder", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.CancelReminderResponse {
		return &daemonv1.CancelReminderResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "CancelReminder", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetReminderId() == "" {
		return nil, status.Error(codes.InvalidArgument, "reminder_id is required")
	}
	current, err := s.store.GetReminder(ctx, req.GetReminderId())
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "reminder not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get reminder: %v", err)
	}
	if current.CancelToken != "" && req.GetCancelToken() != "" && current.CancelToken != req.GetCancelToken() {
		return &daemonv1.CancelReminderResponse{Accepted: false, Reminder: reminderToProto(current)}, nil
	}
	actorType, actorID := reminderActorFromContext(req.GetContext())
	updated, err := s.store.CancelReminder(ctx, req.GetReminderId(), actorType, actorID, "canceled")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "cancel reminder: %v", err)
	}
	if err := s.RecordReminderMutation(ctx, updated, daemonv1.EventOperation_EVENT_OPERATION_CANCELED); err != nil {
		return nil, status.Errorf(codes.Internal, "append reminder event: %v", err)
	}
	resp := &daemonv1.CancelReminderResponse{Accepted: true, Reminder: reminderToProto(updated)}
	remember(ctx, s, "CancelReminder", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) SnoozeReminder(ctx context.Context, req *daemonv1.SnoozeReminderRequest) (*daemonv1.SnoozeReminderResponse, error) {
	if resp, ok := replay(ctx, s, "SnoozeReminder", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.SnoozeReminderResponse {
		return &daemonv1.SnoozeReminderResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "SnoozeReminder", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetReminderId() == "" || req.GetDelaySeconds() == 0 {
		return nil, status.Error(codes.InvalidArgument, "reminder_id and delay_seconds are required")
	}
	actorType, actorID := reminderActorFromContext(req.GetContext())
	nextRun := time.Now().Add(time.Duration(req.GetDelaySeconds()) * time.Second).Unix()
	updated, err := s.store.SnoozeReminder(ctx, req.GetReminderId(), nextRun,
		fmt.Sprintf("in %ds", req.GetDelaySeconds()), actorType, actorID, "snoozed")
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "reminder not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "snooze reminder: %v", err)
	}
	if err := s.RecordReminderMutation(ctx, updated, daemonv1.EventOperation_EVENT_OPERATION_UPDATED); err != nil {
		return nil, status.Errorf(codes.Internal, "append reminder event: %v", err)
	}
	resp := &daemonv1.SnoozeReminderResponse{Reminder: reminderToProto(updated)}
	remember(ctx, s, "SnoozeReminder", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) UpdateReminder(ctx context.Context, req *daemonv1.UpdateReminderRequest) (*daemonv1.UpdateReminderResponse, error) {
	if resp, ok := replay(ctx, s, "UpdateReminder", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.UpdateReminderResponse {
		return &daemonv1.UpdateReminderResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "UpdateReminder", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetReminderId() == "" {
		return nil, status.Error(codes.InvalidArgument, "reminder_id is required")
	}
	patch, err := reminderPatchFromUpdateRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	actorType, actorID := reminderActorFromContext(req.GetContext())
	updated, err := s.store.UpdateReminder(ctx, req.GetReminderId(), patch, actorType, actorID, "updated")
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "reminder not found")
	}
	if errors.Is(err, storage.ErrInvalidState) {
		return nil, status.Error(codes.InvalidArgument, "invalid reminder update")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update reminder: %v", err)
	}
	if err := s.RecordReminderMutation(ctx, updated, daemonv1.EventOperation_EVENT_OPERATION_UPDATED); err != nil {
		return nil, status.Errorf(codes.Internal, "append reminder event: %v", err)
	}
	resp := &daemonv1.UpdateReminderResponse{Reminder: reminderToProto(updated)}
	remember(ctx, s, "UpdateReminder", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) GetReminderLog(ctx context.Context, req *daemonv1.GetReminderLogRequest) (*daemonv1.GetReminderLogResponse, error) {
	if req.GetReminderId() == "" {
		return nil, status.Error(codes.InvalidArgument, "reminder_id is required")
	}
	events, err := s.store.ListReminderEvents(ctx, req.GetReminderId(), 100)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "reminder not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list reminder log: %v", err)
	}
	out := make([]*daemonv1.ReminderEvent, 0, len(events))
	for _, event := range events {
		out = append(out, reminderEventToProto(event))
	}
	return &daemonv1.GetReminderLogResponse{Events: out}, nil
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
	now := unixNow()
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
		statuses = append(statuses, s.derivedAgentStatusLocked(snapshot, now))
		if len(statuses) >= limit {
			break
		}
	}
	return &daemonv1.ListAgentStatusesResponse{Statuses: statuses, NextCursor: cursorFromCount(len(statuses), req.GetTarget(), s.serverID)}, nil
}

func (s *Server) ListAgentProfiles(_ context.Context, req *daemonv1.ListAgentProfilesRequest) (*daemonv1.ListAgentProfilesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := unixNow()
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	profiles := make([]*daemonv1.AgentProfile, 0, limit)
	for _, computer := range s.computers {
		computerStatus := computer.derivedStatus(now)
		for _, profile := range computer.inventory.GetAgents() {
			if profile.GetAgentId() == "" {
				continue
			}
			cp := proto.Clone(profile).(*daemonv1.AgentProfile)
			applyComputerStatusToAgentProfile(cp, computerStatus, computer.lastHeartbeat, now)
			profiles = append(profiles, cp)
			if len(profiles) >= limit {
				return &daemonv1.ListAgentProfilesResponse{Profiles: profiles, NextCursor: cursorFromCount(len(profiles), "", s.serverID)}, nil
			}
		}
	}
	for _, snapshot := range s.statuses {
		profiles = append(profiles, agentProfileFromStatus(s.derivedAgentStatusLocked(snapshot, now)))
		if len(profiles) >= limit {
			break
		}
	}
	return &daemonv1.ListAgentProfilesResponse{Profiles: profiles, NextCursor: cursorFromCount(len(profiles), "", s.serverID)}, nil
}

func (s *Server) ControlAgent(ctx context.Context, req *daemonv1.ControlAgentRequest) (*daemonv1.ControlAgentResponse, error) {
	if resp, ok := replay(ctx, s, "ControlAgent", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.ControlAgentResponse {
		return &daemonv1.ControlAgentResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "ControlAgent", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetAgentId() == "" || req.GetAction() == daemonv1.AgentControlAction_AGENT_CONTROL_ACTION_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "agent_id and action are required")
	}
	computerID := req.GetComputerId()
	runtimeProfileID := req.GetRuntimeProfileId()
	s.mu.Lock()
	if computerID == "" || runtimeProfileID == "" {
		foundComputerID, foundRuntimeProfileID := s.agentRouteLocked(req.GetAgentId())
		if computerID == "" {
			computerID = foundComputerID
		}
		if runtimeProfileID == "" {
			runtimeProfileID = foundRuntimeProfileID
		}
	}
	if computerID == "" {
		s.mu.Unlock()
		return nil, status.Error(codes.NotFound, "agent computer not found")
	}
	now := unixNow()
	operation := &daemonv1.AgentControlOperation{
		OperationId:        storage.NewID("ctl"),
		AgentId:            req.GetAgentId(),
		ComputerId:         computerID,
		RuntimeProfileId:   runtimeProfileID,
		Action:             req.GetAction(),
		State:              daemonv1.AgentControlState_AGENT_CONTROL_STATE_QUEUED,
		Reason:             req.GetReason(),
		RequestedByAgentId: req.GetRequestedByAgentId(),
		CreatedTimeUnix:    now,
		UpdatedTimeUnix:    now,
	}
	leaseTTL := defaultTTL(req.GetLeaseTtlSeconds(), 300)
	lease := newLease("agent_control", operation.GetOperationId(), req.GetAgentId(), int64(leaseTTL))
	s.leases[lease.GetLeaseId()] = lease
	s.controls[operation.GetOperationId()] = proto.Clone(operation).(*daemonv1.AgentControlOperation)
	event := s.serverEventLocked(&daemonv1.ServerEvent{
		AggregateId: operation.GetOperationId(),
		Target:      operation.GetAgentId(),
		Kind:        daemonv1.ServerEventKind_SERVER_EVENT_KIND_AGENT_CONTROL,
		RequestId:   req.GetRequestId(),
		Operation:   daemonv1.EventOperation_EVENT_OPERATION_CREATED,
		Scope: &daemonv1.EventScope{
			ScopeType: daemonv1.EventScopeType_EVENT_SCOPE_TYPE_AGENT,
			ScopeId:   operation.GetAgentId(),
			Target:    operation.GetAgentId(),
		},
		Payload: &daemonv1.ServerEvent_AgentControl{AgentControl: proto.Clone(operation).(*daemonv1.AgentControlOperation)},
	})
	s.serverEvents[event.GetEventId()] = event
	profile := s.agentProfileLocked(req.GetAgentId())
	s.mu.Unlock()

	resp := &daemonv1.ControlAgentResponse{Accepted: true, Operation: operation, Profile: profile, Lease: lease}
	remember(ctx, s, "ControlAgent", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) SendAgentDirectMessage(ctx context.Context, req *daemonv1.SendAgentDirectMessageRequest) (*daemonv1.SendAgentDirectMessageResponse, error) {
	if resp, ok := replay(ctx, s, "SendAgentDirectMessage", req.GetRequestId(), req.GetIdempotencyKey(), func() *daemonv1.SendAgentDirectMessageResponse {
		return &daemonv1.SendAgentDirectMessageResponse{}
	}); ok {
		return resp, nil
	}
	if err := reserve(ctx, s, "SendAgentDirectMessage", req.GetRequestId(), req.GetIdempotencyKey()); err != nil {
		return nil, err
	}
	if req.GetAgentId() == "" || req.GetContent() == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id and content are required")
	}
	s.mu.Lock()
	computerID, _ := s.agentRouteLocked(req.GetAgentId())
	s.mu.Unlock()
	if computerID == "" {
		return nil, status.Error(codes.NotFound, "agent computer not found")
	}
	target := "dm:" + req.GetAgentId()
	sender := req.GetSender()
	if sender == nil {
		sender = &daemonv1.Actor{ActorKind: daemonv1.ActorKind_ACTOR_KIND_HUMAN, DisplayName: "User"}
	}
	messageResp, err := s.SendMessage(ctx, &daemonv1.SendMessageRequest{
		Target:           target,
		Content:          req.GetContent(),
		ReplyToMessageId: req.GetReplyToMessageId(),
		AttachmentIds:    append([]string(nil), req.GetAttachmentIds()...),
		RequestId:        req.GetRequestId() + "-message",
		IdempotencyKey:   req.GetIdempotencyKey() + "-message",
		Context:          req.GetContext(),
		Sender:           sender,
		Role:             "user",
	})
	if err != nil {
		return nil, err
	}
	message := messageResp.GetMessage()
	s.mu.Lock()
	event := s.serverEventLocked(&daemonv1.ServerEvent{
		AggregateId: req.GetAgentId(),
		Target:      target,
		Kind:        daemonv1.ServerEventKind_SERVER_EVENT_KIND_MESSAGE,
		RequestId:   req.GetRequestId(),
		Operation:   daemonv1.EventOperation_EVENT_OPERATION_APPENDED,
		Scope: &daemonv1.EventScope{
			ScopeType: daemonv1.EventScopeType_EVENT_SCOPE_TYPE_AGENT,
			ScopeId:   req.GetAgentId(),
			Target:    target,
		},
		Payload: &daemonv1.ServerEvent_Message{Message: proto.Clone(message).(*daemonv1.CollaborationMessage)},
	})
	s.serverEvents[event.GetEventId()] = event
	s.mu.Unlock()

	resp := &daemonv1.SendAgentDirectMessageResponse{Accepted: true, Message: message}
	remember(ctx, s, "SendAgentDirectMessage", req.GetRequestId(), req.GetIdempotencyKey(), resp)
	return resp, nil
}

func (s *Server) ListComputerInventories(limit int) []ComputerInventorySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := unixNow()
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	keys := make([]string, 0, len(s.computers))
	for computerID := range s.computers {
		keys = append(keys, computerID)
	}
	sort.Strings(keys)
	out := make([]ComputerInventorySnapshot, 0, min(limit, len(keys)))
	for _, computerID := range keys {
		computer := s.computers[computerID]
		computerStatus := computer.derivedStatus(now)
		snapshot := ComputerInventorySnapshot{
			InventoryVersion:  computer.inventoryVersion,
			LastHeartbeatUnix: computer.lastHeartbeat,
		}
		if computer.info != nil {
			snapshot.Info = proto.Clone(computer.info).(*daemonv1.ComputerInfo)
			snapshot.Info.Status = computerStatus
			if computer.lastHeartbeat > 0 {
				snapshot.Info.LastSeenUnix = computer.lastHeartbeat
			}
		}
		if computer.inventory != nil {
			snapshot.Inventory = proto.Clone(computer.inventory).(*daemonv1.ComputerInventory)
			for _, profile := range snapshot.Inventory.GetAgents() {
				applyComputerStatusToAgentProfile(profile, computerStatus, computer.lastHeartbeat, now)
			}
		}
		out = append(out, snapshot)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Server) CreateAgentInstance(input CreateAgentInstanceInput) (CreateAgentInstanceResult, error) {
	input.ComputerID = strings.TrimSpace(input.ComputerID)
	input.RuntimeID = strings.TrimSpace(input.RuntimeID)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Name = strings.TrimSpace(input.Name)
	input.Target = strings.TrimSpace(input.Target)
	if input.ComputerID == "" || input.RuntimeID == "" || input.TemplateID == "" {
		return CreateAgentInstanceResult{}, status.Error(codes.InvalidArgument, "computerId, runtimeId, and templateId are required")
	}
	if input.DisplayName == "" {
		return CreateAgentInstanceResult{}, status.Error(codes.InvalidArgument, "displayName is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	computer := s.computers[input.ComputerID]
	if computer == nil || computer.inventory == nil {
		return CreateAgentInstanceResult{}, status.Error(codes.NotFound, "computer inventory not found")
	}
	runtimeEntry := findRuntime(computer.inventory, input.RuntimeID)
	if runtimeEntry == nil {
		return CreateAgentInstanceResult{}, status.Error(codes.NotFound, "runtime not found")
	}
	if !runtimeEntry.GetInstalled() || !runtimeEntry.GetHealthy() {
		return CreateAgentInstanceResult{}, status.Error(codes.FailedPrecondition, "runtime is not available")
	}
	templateProfile := findRuntimeProfile(computer.inventory, input.TemplateID)
	if templateProfile == nil {
		return CreateAgentInstanceResult{}, status.Error(codes.NotFound, "runtime template not found")
	}
	adapter, err := parseAdapterConfig(templateProfile.GetAdapterConfigJson())
	if err != nil {
		return CreateAgentInstanceResult{}, status.Error(codes.InvalidArgument, err.Error())
	}
	template := adapter.Template
	if template.RuntimeKind == "" {
		template.RuntimeKind = templateProfile.GetKind()
	}
	if template.RuntimeKind != "" && runtimeEntry.GetKind() != "" && template.RuntimeKind != runtimeEntry.GetKind() {
		return CreateAgentInstanceResult{}, status.Error(codes.InvalidArgument, "runtime/template kind mismatch")
	}
	if !template.MultiInstance {
		return CreateAgentInstanceResult{}, status.Error(codes.FailedPrecondition, "runtime template does not allow multiple instances")
	}
	optionValues := normalizedAgentOptions(template, input.DisplayName, input.Options)
	wrap, err := runtimeadapter.BuildWrapCommand(template, optionValues)
	if err != nil {
		return CreateAgentInstanceResult{}, status.Error(codes.InvalidArgument, err.Error())
	}

	randomID := storage.NewID("agent")
	agentID := randomID
	slug := sanitizeAgentSlug(firstNonEmpty(input.Name, input.DisplayName))
	if slug != "" {
		agentID = "agent_" + slug + "_" + strings.TrimPrefix(randomID, "agent_")
	}
	instanceProfileID := "profile_" + strings.TrimPrefix(agentID, "agent_")
	adapterJSON, err := instanceAdapterConfig(adapter, optionValues, wrap, agentID, input.Target)
	if err != nil {
		return CreateAgentInstanceResult{}, status.Errorf(codes.Internal, "build agent adapter config: %v", err)
	}
	profile := proto.Clone(templateProfile).(*daemonv1.RuntimeProfile)
	profile.RuntimeProfileId = instanceProfileID
	profile.AdapterConfigJson = adapterJSON
	profile.Model = firstNonEmpty(optionValue(template.Options, optionValues, "model"), templateProfile.GetModel())
	agent := &daemonv1.AgentProfile{
		AgentId:          agentID,
		Name:             firstNonEmpty(input.Name, agentID),
		DisplayName:      input.DisplayName,
		Description:      template.Description,
		Enabled:          true,
		Provider:         templateProfile.GetProvider(),
		Model:            profile.GetModel(),
		ComputerId:       input.ComputerID,
		RuntimeProfileId: instanceProfileID,
		RuntimeKind:      runtimeEntry.GetKind(),
		ReasoningEffort:  optionValue(template.Options, optionValues, "reasoning_effort"),
		Status:           daemonv1.AgentPresence_AGENT_PRESENCE_IDLE,
		Capabilities:     cloneCapabilities(templateProfile.GetCapabilities()),
	}
	computer.inventory.RuntimeProfiles = append(computer.inventory.RuntimeProfiles, profile)
	computer.inventory.Agents = append(computer.inventory.Agents, agent)
	computer.inventoryVersion = strconv.FormatInt(unixNow(), 10)
	return CreateAgentInstanceResult{
		Agent:          proto.Clone(agent).(*daemonv1.AgentProfile),
		RuntimeProfile: proto.Clone(profile).(*daemonv1.RuntimeProfile),
	}, nil
}

func findRuntime(inventory *daemonv1.ComputerInventory, runtimeID string) *daemonv1.Runtime {
	for _, runtimeEntry := range inventory.GetRuntimes() {
		if runtimeEntry.GetRuntimeId() == runtimeID {
			return runtimeEntry
		}
	}
	return nil
}

func findRuntimeProfile(inventory *daemonv1.ComputerInventory, profileID string) *daemonv1.RuntimeProfile {
	for _, profile := range inventory.GetRuntimeProfiles() {
		if profile.GetRuntimeProfileId() == profileID {
			return profile
		}
	}
	return nil
}

func parseAdapterConfig(configJSON string) (runtimeadapter.AdapterConfig, error) {
	var cfg runtimeadapter.AdapterConfig
	if strings.TrimSpace(configJSON) == "" {
		return cfg, errors.New("runtime profile is missing adapter config")
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return cfg, fmt.Errorf("runtime profile adapter config is invalid: %w", err)
	}
	if cfg.SchemaVersion != "" && cfg.SchemaVersion != runtimeadapter.SchemaVersion {
		return cfg, fmt.Errorf("unsupported runtime adapter schema %q", cfg.SchemaVersion)
	}
	if strings.TrimSpace(cfg.Template.TemplateID) == "" {
		return cfg, errors.New("runtime profile adapter config is missing template")
	}
	return cfg, nil
}

func normalizedAgentOptions(template runtimeadapter.InstanceTemplate, displayName string, values map[string]string) map[string]string {
	clean := make(map[string]string, len(values)+1)
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		clean[key] = strings.TrimSpace(value)
	}
	if strings.TrimSpace(clean["display_name"]) == "" {
		clean["display_name"] = strings.TrimSpace(displayName)
	}
	for _, option := range template.Options {
		if _, ok := clean[option.Name]; !ok && option.Default != "" {
			clean[option.Name] = option.Default
		}
	}
	return clean
}

func instanceAdapterConfig(
	base runtimeadapter.AdapterConfig,
	options map[string]string,
	wrap runtimeadapter.WrapCommand,
	agentID string,
	target string,
) (string, error) {
	base.Template.InventoryRole = "agent_instance"
	payload := struct {
		runtimeadapter.AdapterConfig
		AgentID          string                     `json:"agentId"`
		Target           string                     `json:"target,omitempty"`
		SourceTemplateID string                     `json:"sourceTemplateId"`
		SelectedOptions  map[string]string          `json:"selectedOptions"`
		WrapCommand      runtimeadapter.WrapCommand `json:"wrapCommand"`
	}{
		AdapterConfig:    base,
		AgentID:          agentID,
		Target:           strings.TrimSpace(target),
		SourceTemplateID: base.Template.TemplateID,
		SelectedOptions:  redactedAgentOptions(base.Template, options),
		WrapCommand:      wrap,
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func redactedAgentOptions(template runtimeadapter.InstanceTemplate, values map[string]string) map[string]string {
	sensitive := make(map[string]bool, len(template.Options))
	for _, option := range template.Options {
		if option.Sensitive {
			sensitive[option.Name] = true
		}
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if sensitive[key] && value != "" {
			out[key] = "<redacted>"
			continue
		}
		out[key] = value
	}
	return out
}

func optionValue(options []runtimeadapter.OptionSchema, values map[string]string, name string) string {
	if value := strings.TrimSpace(values[name]); value != "" {
		return value
	}
	for _, option := range options {
		if option.Name == name {
			return option.Default
		}
	}
	return ""
}

func cloneCapabilities(capabilities []*daemonv1.Capability) []*daemonv1.Capability {
	out := make([]*daemonv1.Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability == nil {
			continue
		}
		out = append(out, proto.Clone(capability).(*daemonv1.Capability))
	}
	return out
}

func sanitizeAgentSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (c *computerState) derivedStatus(now int64) daemonv1.ComputerStatus {
	reported := daemonv1.ComputerStatus_COMPUTER_STATUS_UNSPECIFIED
	if c != nil && c.info != nil {
		reported = c.info.GetStatus()
	}
	if c == nil || c.lastHeartbeat <= 0 {
		return reported
	}
	ageSeconds := now - c.lastHeartbeat
	if ageSeconds >= computerOfflineAfterSeconds {
		return daemonv1.ComputerStatus_COMPUTER_STATUS_OFFLINE
	}
	if ageSeconds >= computerStaleAfterSeconds {
		return daemonv1.ComputerStatus_COMPUTER_STATUS_STALE
	}
	switch reported {
	case daemonv1.ComputerStatus_COMPUTER_STATUS_UNSPECIFIED,
		daemonv1.ComputerStatus_COMPUTER_STATUS_STALE,
		daemonv1.ComputerStatus_COMPUTER_STATUS_OFFLINE:
		return daemonv1.ComputerStatus_COMPUTER_STATUS_ONLINE
	default:
		return reported
	}
}

func (s *Server) derivedAgentStatusLocked(snapshot *daemonv1.AgentStatusSnapshot, now int64) *daemonv1.AgentStatusSnapshot {
	cp := proto.Clone(snapshot).(*daemonv1.AgentStatusSnapshot)
	if cp.UpdatedTimeUnix == 0 {
		cp.UpdatedTimeUnix = now
	}
	computer := s.computers[cp.GetComputerId()]
	if computer == nil {
		return cp
	}
	return applyComputerStatusToAgentStatus(cp, computer.derivedStatus(now), computer.lastHeartbeat, now)
}

func applyComputerStatusToAgentProfile(profile *daemonv1.AgentProfile, computerStatus daemonv1.ComputerStatus, lastHeartbeat int64, now int64) {
	if profile == nil {
		return
	}
	switch computerStatus {
	case daemonv1.ComputerStatus_COMPUTER_STATUS_STALE:
		profile.Status = daemonv1.AgentPresence_AGENT_PRESENCE_STALE
	case daemonv1.ComputerStatus_COMPUTER_STATUS_OFFLINE:
		profile.Status = daemonv1.AgentPresence_AGENT_PRESENCE_OFFLINE
	case daemonv1.ComputerStatus_COMPUTER_STATUS_DEGRADED:
		if profile.GetStatus() == daemonv1.AgentPresence_AGENT_PRESENCE_UNSPECIFIED ||
			profile.GetStatus() == daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE ||
			profile.GetStatus() == daemonv1.AgentPresence_AGENT_PRESENCE_IDLE ||
			profile.GetStatus() == daemonv1.AgentPresence_AGENT_PRESENCE_BUSY {
			profile.Status = daemonv1.AgentPresence_AGENT_PRESENCE_DEGRADED
		}
	}
	if profile.LastActivityTimeUnix == 0 && lastHeartbeat > 0 {
		profile.LastActivityTimeUnix = lastHeartbeat
	}
	if profile.StatusSnapshot == nil {
		if computerStatus != daemonv1.ComputerStatus_COMPUTER_STATUS_STALE &&
			computerStatus != daemonv1.ComputerStatus_COMPUTER_STATUS_OFFLINE &&
			computerStatus != daemonv1.ComputerStatus_COMPUTER_STATUS_DEGRADED {
			return
		}
		profile.StatusSnapshot = &daemonv1.AgentStatusSnapshot{
			AgentId:          profile.GetAgentId(),
			ComputerId:       profile.GetComputerId(),
			RuntimeProfileId: profile.GetRuntimeProfileId(),
			Presence:         profile.GetStatus(),
			UpdatedTimeUnix:  firstNonZeroInt64(lastHeartbeat, now),
		}
	}
	profile.StatusSnapshot = applyComputerStatusToAgentStatus(profile.StatusSnapshot, computerStatus, lastHeartbeat, now)
	profile.Status = profile.StatusSnapshot.GetPresence()
}

func applyComputerStatusToAgentStatus(snapshot *daemonv1.AgentStatusSnapshot, computerStatus daemonv1.ComputerStatus, lastHeartbeat int64, now int64) *daemonv1.AgentStatusSnapshot {
	if snapshot == nil {
		return nil
	}
	if snapshot.UpdatedTimeUnix == 0 {
		snapshot.UpdatedTimeUnix = firstNonZeroInt64(lastHeartbeat, now)
	}
	switch computerStatus {
	case daemonv1.ComputerStatus_COMPUTER_STATUS_STALE:
		snapshot.Presence = daemonv1.AgentPresence_AGENT_PRESENCE_STALE
		if snapshot.Severity == daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_UNSPECIFIED ||
			snapshot.Severity == daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO {
			snapshot.Severity = daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_WARNING
		}
		if snapshot.GetSummary() == "" ||
			snapshot.GetHealth() == daemonv1.AgentHealth_AGENT_HEALTH_OK ||
			snapshot.GetHealth() == daemonv1.AgentHealth_AGENT_HEALTH_UNSPECIFIED {
			snapshot.Summary = "daemon heartbeat stale"
		}
		snapshot.Detail = heartbeatAgeDetail(lastHeartbeat, now)
	case daemonv1.ComputerStatus_COMPUTER_STATUS_OFFLINE:
		snapshot.Presence = daemonv1.AgentPresence_AGENT_PRESENCE_OFFLINE
		snapshot.ActivityState = daemonv1.AgentActivityState_AGENT_ACTIVITY_STATE_WAITING
		snapshot.Health = daemonv1.AgentHealth_AGENT_HEALTH_OFFLINE
		snapshot.Severity = daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_ERROR
		snapshot.Summary = "daemon offline"
		snapshot.Detail = heartbeatAgeDetail(lastHeartbeat, now)
	case daemonv1.ComputerStatus_COMPUTER_STATUS_DEGRADED:
		if snapshot.GetPresence() == daemonv1.AgentPresence_AGENT_PRESENCE_UNSPECIFIED ||
			snapshot.GetPresence() == daemonv1.AgentPresence_AGENT_PRESENCE_ONLINE ||
			snapshot.GetPresence() == daemonv1.AgentPresence_AGENT_PRESENCE_IDLE ||
			snapshot.GetPresence() == daemonv1.AgentPresence_AGENT_PRESENCE_BUSY {
			snapshot.Presence = daemonv1.AgentPresence_AGENT_PRESENCE_DEGRADED
		}
		if snapshot.Severity == daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_UNSPECIFIED ||
			snapshot.Severity == daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_INFO {
			snapshot.Severity = daemonv1.AgentStatusSeverity_AGENT_STATUS_SEVERITY_WARNING
		}
	}
	return snapshot
}

func heartbeatAgeDetail(lastHeartbeat int64, now int64) string {
	if lastHeartbeat <= 0 {
		return "daemon heartbeat has not been observed"
	}
	return fmt.Sprintf("last daemon heartbeat was %d seconds ago", max(int64(0), now-lastHeartbeat))
}

func (s *Server) AcknowledgeServerEvents(_ context.Context, req *daemonv1.AcknowledgeServerEventsRequest) (*daemonv1.AcknowledgeServerEventsResponse, error) {
	s.mu.Lock()
	for _, eventID := range req.GetEventIds() {
		delete(s.serverEvents, eventID)
	}
	s.mu.Unlock()
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
	sent := map[string]bool{}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			for _, event := range s.pendingServerEvents(req, sent) {
				if err := stream.Send(&daemonv1.SubscribeServerEventsResponse{Event: event}); err != nil {
					return err
				}
				sent[event.GetEventId()] = true
			}
		}
	}
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

func shouldEnqueueSourceDelivery(req *daemonv1.SendMessageRequest, senderKind string) bool {
	switch req.GetOutboundPolicy() {
	case daemonv1.OutboundPolicy_OUTBOUND_POLICY_NONE:
		return false
	case daemonv1.OutboundPolicy_OUTBOUND_POLICY_SOURCE_ONLY,
		daemonv1.OutboundPolicy_OUTBOUND_POLICY_ALL_BOUND_ENDPOINTS,
		daemonv1.OutboundPolicy_OUTBOUND_POLICY_SELECTED_ENDPOINTS:
		return true
	default:
		return req.GetEmitOutbound() || senderKind == "agent"
	}
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
	aggregateID := firstNonEmpty(msg.ThreadID, protoMsg.GetAggregateId(), msg.Target)
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

func (s *Server) EnqueueSourceOutboundDelivery(ctx context.Context, msg storage.Message) (storage.OutboundDelivery, error) {
	if s == nil || s.store == nil {
		return storage.OutboundDelivery{}, nil
	}
	source, ok, err := s.sourceMessageForOutbound(ctx, msg)
	if err != nil || !ok {
		return storage.OutboundDelivery{}, err
	}
	if source.SourceEndpointID == "" || source.ExternalMessageID == "" {
		return storage.OutboundDelivery{}, nil
	}
	endpoint, err := s.store.GetInteractionEndpoint(ctx, source.SourceEndpointID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.OutboundDelivery{}, nil
	}
	if err != nil {
		return storage.OutboundDelivery{}, err
	}
	if !endpoint.OutboundEnabled {
		return storage.OutboundDelivery{}, nil
	}
	delivery, err := s.store.CreateOutboundDelivery(ctx, storage.OutboundDelivery{
		Target:            msg.Target,
		MessageID:         msg.ID,
		EndpointID:        endpoint.ID,
		EndpointKind:      endpoint.Kind,
		ExternalMessageID: source.ExternalMessageID,
		Status:            "pending",
		RequestID:         firstNonEmpty(msg.RequestID, msg.ID+":"+endpoint.ID),
	})
	if err != nil {
		return storage.OutboundDelivery{}, err
	}
	s.EmitOutboundDeliveryEvent(delivery, daemonv1.EventOperation_EVENT_OPERATION_CREATED)
	return delivery, nil
}

func (s *Server) sourceMessageForOutbound(ctx context.Context, msg storage.Message) (storage.Message, bool, error) {
	if msg.SourceEndpointID != "" && msg.ExternalMessageID != "" {
		return msg, true, nil
	}
	if msg.ReplyToMessageID == "" {
		return storage.Message{}, false, nil
	}
	parent, err := s.store.GetMessage(ctx, msg.Target, msg.ReplyToMessageID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Message{}, false, nil
	}
	if err != nil {
		return storage.Message{}, false, err
	}
	if parent.SourceEndpointID != "" && parent.ExternalMessageID != "" {
		return parent, true, nil
	}
	if parent.ThreadID == "" || parent.ThreadID == parent.ID {
		return storage.Message{}, false, nil
	}
	root, err := s.store.GetMessage(ctx, parent.Target, parent.ThreadID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Message{}, false, nil
	}
	if err != nil {
		return storage.Message{}, false, err
	}
	if root.SourceEndpointID != "" && root.ExternalMessageID != "" {
		return root, true, nil
	}
	return storage.Message{}, false, nil
}

func (s *Server) EmitOutboundDeliveryEvent(delivery storage.OutboundDelivery, operation daemonv1.EventOperation) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event := s.serverEventLocked(&daemonv1.ServerEvent{
		AggregateId: delivery.MessageID,
		Target:      delivery.Target,
		Kind:        daemonv1.ServerEventKind_SERVER_EVENT_KIND_OUTBOUND_DELIVERY,
		Operation:   operation,
		Scope: &daemonv1.EventScope{
			ScopeType: daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET,
			ScopeId:   delivery.Target,
			Target:    delivery.Target,
		},
		RequestId:       delivery.RequestID,
		Payload:         &daemonv1.ServerEvent_OutboundDelivery{OutboundDelivery: outboundDeliveryToProto(delivery)},
		ProtocolVersion: protocolVersion,
	})
	s.serverEvents[event.GetEventId()] = event
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

func (s *Server) RecordReminderMutation(ctx context.Context, reminder storage.Reminder, operation daemonv1.EventOperation) error {
	if s == nil || s.store == nil {
		return nil
	}
	protoReminder := reminderToProto(reminder)
	payload, err := protojson.Marshal(protoReminder)
	if err != nil {
		return err
	}
	_, err = s.store.AppendCollaborationEvent(ctx, storage.CollaborationEvent{
		ServerID:        s.serverID,
		Target:          reminder.Target,
		AggregateID:     reminder.ID,
		Kind:            "reminder",
		Operation:       eventOperationToStorage(operation),
		ScopeType:       eventScopeTypeToStorage(daemonv1.EventScopeType_EVENT_SCOPE_TYPE_TARGET),
		ScopeID:         reminder.Target,
		PayloadJSON:     string(payload),
		CreatedUnix:     reminder.UpdatedUnix,
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
	} else if reminder, ok := reminderFromEvent(event); ok {
		out.Payload = &daemonv1.CollaborationEvent_Reminder{Reminder: reminder}
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

func reminderFromEvent(event storage.CollaborationEvent) (*daemonv1.ReminderRecord, bool) {
	if event.Kind != "reminder" {
		return nil, false
	}
	reminder := &daemonv1.ReminderRecord{}
	if err := protojson.Unmarshal([]byte(event.PayloadJSON), reminder); err != nil {
		return nil, false
	}
	return reminder, true
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
	attachments := make([]*daemonv1.AttachmentRecord, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		attachments = append(attachments, attachmentToProto(attachment))
	}
	return &daemonv1.CollaborationMessage{
		MessageId:         msg.ID,
		Target:            msg.Target,
		ThreadId:          msg.ThreadID,
		Role:              msg.Role,
		Content:           msg.Content,
		ReplyToMessageId:  msg.ReplyToMessageID,
		CreatedTimeUnix:   msg.CreatedUnix,
		RequestId:         msg.RequestID,
		SourceEndpointId:  msg.SourceEndpointID,
		ExternalMessageId: msg.ExternalMessageID,
		MetadataJson:      msg.MetadataJSON,
		Attachments:       attachments,
		Sender: &daemonv1.Actor{
			ActorKind:   actorKindFromStorage(msg.SenderKind),
			AgentId:     msg.SenderAgentID,
			UserId:      msg.SenderUserID,
			DisplayName: msg.SenderDisplayName,
		},
		AggregateId: msg.Target,
	}
}

func outboundDeliveryToProto(delivery storage.OutboundDelivery) *daemonv1.OutboundDeliveryRecord {
	return &daemonv1.OutboundDeliveryRecord{
		DeliveryId:        delivery.ID,
		Target:            delivery.Target,
		MessageId:         delivery.MessageID,
		EndpointId:        delivery.EndpointID,
		EndpointKind:      delivery.EndpointKind,
		ExternalMessageId: delivery.ExternalMessageID,
		Status:            outboundDeliveryStatusFromStorage(delivery.Status),
		AttemptCount:      delivery.AttemptCount,
		NextRetryTimeUnix: delivery.NextRetryTimeUnix,
		DeliveredTimeUnix: delivery.DeliveredTimeUnix,
		LastError:         delivery.LastError,
		RequestId:         delivery.RequestID,
	}
}

func attachmentToProto(attachment storage.Attachment) *daemonv1.AttachmentRecord {
	return &daemonv1.AttachmentRecord{
		AttachmentId:    attachment.ID,
		Target:          attachment.Target,
		OwnerId:         attachment.OwnerID,
		Filename:        attachment.Filename,
		MimeType:        attachment.MimeType,
		SizeBytes:       attachment.SizeBytes,
		StorageRef:      attachment.StorageRef,
		DownloadUrl:     attachment.DownloadURL,
		UploadUrl:       attachment.UploadURL,
		ExpiresTimeUnix: attachment.ExpiresTimeUnix,
		CreatedTimeUnix: attachment.CreatedUnix,
	}
}

func savedMessageToProto(saved storage.SavedMessage) *daemonv1.SavedMessageRecord {
	return &daemonv1.SavedMessageRecord{
		SavedMessageId: saved.ID,
		Target:         saved.Target,
		ThreadId:       saved.Message.ThreadID,
		MessageId:      saved.MessageID,
		SavedByAgentId: saved.SavedByAgentID,
		SavedByUserId:  saved.SavedByUserID,
		SavedTimeUnix:  saved.CreatedUnix,
		Message:        messageToProto(saved.Message),
	}
}

func messageSearchSortToStorage(sort daemonv1.MessageSearchSort) string {
	switch sort {
	case daemonv1.MessageSearchSort_MESSAGE_SEARCH_SORT_RECENT:
		return "recent"
	case daemonv1.MessageSearchSort_MESSAGE_SEARCH_SORT_RELEVANCE:
		return "relevance"
	default:
		return ""
	}
}

func outboundDeliveryStatusToStorage(statusValue daemonv1.OutboundDeliveryStatus) string {
	switch statusValue {
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING:
		return "pending"
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED:
		return "delivered"
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_FAILED:
		return "failed"
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_RETRYING:
		return "retrying"
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_CANCELED:
		return "canceled"
	default:
		return ""
	}
}

func outboundDeliveryStatusFromStorage(statusValue string) daemonv1.OutboundDeliveryStatus {
	switch strings.ToLower(strings.TrimSpace(statusValue)) {
	case "pending":
		return daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_PENDING
	case "delivered":
		return daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_DELIVERED
	case "failed":
		return daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_FAILED
	case "retrying":
		return daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_RETRYING
	case "canceled", "cancelled":
		return daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_CANCELED
	default:
		return daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_UNSPECIFIED
	}
}

func outboundDeliveryOperation(statusValue daemonv1.OutboundDeliveryStatus) daemonv1.EventOperation {
	switch statusValue {
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_FAILED:
		return daemonv1.EventOperation_EVENT_OPERATION_FAILED
	case daemonv1.OutboundDeliveryStatus_OUTBOUND_DELIVERY_STATUS_CANCELED:
		return daemonv1.EventOperation_EVENT_OPERATION_CANCELED
	default:
		return daemonv1.EventOperation_EVENT_OPERATION_STATE_CHANGED
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

func (s *Server) agentRouteLocked(agentID string) (string, string) {
	for computerID, computer := range s.computers {
		for _, profile := range computer.inventory.GetAgents() {
			if profile.GetAgentId() == agentID {
				return firstNonEmpty(profile.GetComputerId(), computerID), profile.GetRuntimeProfileId()
			}
		}
	}
	if snapshot := s.statuses[agentID]; snapshot != nil {
		return snapshot.GetComputerId(), snapshot.GetRuntimeProfileId()
	}
	return "", ""
}

func (s *Server) agentProfileLocked(agentID string) *daemonv1.AgentProfile {
	for _, computer := range s.computers {
		for _, profile := range computer.inventory.GetAgents() {
			if profile.GetAgentId() == agentID {
				return proto.Clone(profile).(*daemonv1.AgentProfile)
			}
		}
	}
	if snapshot := s.statuses[agentID]; snapshot != nil {
		return agentProfileFromStatus(snapshot)
	}
	return &daemonv1.AgentProfile{AgentId: agentID, Name: agentID, DisplayName: agentID, Enabled: true}
}

func (s *Server) serverEventLocked(event *daemonv1.ServerEvent) *daemonv1.ServerEvent {
	cp := proto.Clone(event).(*daemonv1.ServerEvent)
	if cp.EventId == "" {
		cp.EventId = storage.NewID("evt")
	}
	if cp.Sequence == 0 {
		s.eventSeq++
		cp.Sequence = s.eventSeq
	}
	if cp.CreatedTimeUnix == 0 {
		cp.CreatedTimeUnix = unixNow()
	}
	if cp.ProtocolVersion == 0 {
		cp.ProtocolVersion = protocolVersion
	}
	return cp
}

func (s *Server) pendingServerEvents(req *daemonv1.SubscribeServerEventsRequest, sent map[string]bool) []*daemonv1.ServerEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]*daemonv1.ServerEvent, 0, len(s.serverEvents))
	for eventID, event := range s.serverEvents {
		if sent[eventID] || !serverEventMatches(req, event) {
			continue
		}
		events = append(events, proto.Clone(event).(*daemonv1.ServerEvent))
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].GetSequence() < events[j].GetSequence()
	})
	return events
}

func serverEventMatches(req *daemonv1.SubscribeServerEventsRequest, event *daemonv1.ServerEvent) bool {
	if event == nil {
		return false
	}
	if len(req.GetKinds()) > 0 && !containsServerEventKind(req.GetKinds(), event.GetKind()) {
		return false
	}
	if req.GetIncludeAllAgents() || req.GetIncludeAllScopes() {
		return true
	}
	switch payload := event.GetPayload().(type) {
	case *daemonv1.ServerEvent_AgentControl:
		op := payload.AgentControl
		if op.GetComputerId() != "" && op.GetComputerId() == req.GetComputerId() {
			return true
		}
		return contains(req.GetAgentIds(), op.GetAgentId())
	case *daemonv1.ServerEvent_Message:
		return contains(req.GetAgentIds(), event.GetAggregateId()) || contains(req.GetTargets(), payload.Message.GetTarget())
	case *daemonv1.ServerEvent_OutboundDelivery:
		delivery := payload.OutboundDelivery
		return contains(req.GetTargets(), delivery.GetTarget())
	case *daemonv1.ServerEvent_Run:
		run := payload.Run
		if run.GetComputerId() != "" && run.GetComputerId() == req.GetComputerId() {
			return true
		}
		return contains(req.GetAgentIds(), run.GetAgentId())
	default:
		return event.GetTarget() == req.GetComputerId()
	}
}

func containsServerEventKind(values []daemonv1.ServerEventKind, needle daemonv1.ServerEventKind) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func taskToProto(task storage.Task) *daemonv1.Task {
	return &daemonv1.Task{
		TaskId:          task.ID,
		Summary:         task.Summary,
		State:           taskStateFromStorage(task.State),
		Target:          task.Target,
		ThreadId:        task.ID,
		AssigneeId:      task.AssigneeID,
		CreatedByUserId: task.CreatedByUserID,
		BlockedReason:   task.BlockedReason,
		BoardColumn:     task.State,
		CreatedTimeUnix: task.CreatedUnix,
		UpdatedTimeUnix: task.UpdatedUnix,
		ClaimPolicy:     daemonv1.TaskClaimPolicy_TASK_CLAIM_POLICY_EXCLUSIVE,
	}
}

func reminderToProto(reminder storage.Reminder) *daemonv1.ReminderRecord {
	out := &daemonv1.ReminderRecord{
		ReminderId:   reminder.ID,
		Target:       reminder.Target,
		ScheduleKind: reminderScheduleKindFromStorage(reminder.ScheduleKind),
		Schedule:     reminder.Schedule,
		Prompt:       reminder.Prompt,
		Enabled:      reminder.Enabled,
		NextRunUnix:  reminder.NextRunUnix,
		LastRunUnix:  reminder.LastRunUnix,
		RunCount:     uint32(reminder.RunCount),
		LastError:    reminder.LastError,
		Title:        reminder.Title,
		Status:       reminderStatusFromStorage(reminder.Status),
		MsgRef:       reminder.MsgRef,
		CancelToken:  reminder.CancelToken,
	}
	if reminder.RecurrenceRule != "" || reminder.RecurrenceDescription != "" || reminder.RecurrenceTimezone != "" {
		out.Recurrence = &daemonv1.ReminderRecurrence{
			Rule:        reminder.RecurrenceRule,
			Description: reminder.RecurrenceDescription,
			Timezone:    reminder.RecurrenceTimezone,
		}
	}
	return out
}

func reminderEventToProto(event storage.ReminderEvent) *daemonv1.ReminderEvent {
	return &daemonv1.ReminderEvent{
		EventId:          event.ID,
		ReminderId:       event.ReminderID,
		EventType:        reminderEventTypeFromStorage(event.EventType),
		ActorType:        reminderActorTypeFromStorage(event.ActorType),
		ActorId:          event.ActorID,
		OccurredTimeUnix: event.OccurredTimeUnix,
		NextFireTimeUnix: event.NextFireTimeUnix,
		Detail:           event.Detail,
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

type reminderPlan struct {
	Kind                  string
	Schedule              string
	NextRunUnix           int64
	RecurrenceRule        string
	RecurrenceDescription string
	RecurrenceTimezone    string
}

func reminderPlanFromScheduleRequest(req *daemonv1.ScheduleReminderRequest) (reminderPlan, error) {
	if req.GetDelaySeconds() > 0 {
		return reminderPlan{
			Kind:               "at",
			Schedule:           fmt.Sprintf("in %ds", req.GetDelaySeconds()),
			NextRunUnix:        time.Now().Add(time.Duration(req.GetDelaySeconds()) * time.Second).Unix(),
			RecurrenceTimezone: req.GetTimezone(),
		}, nil
	}
	if req.GetFireAt() != "" {
		nextRun, err := parseReminderFireAt(req.GetFireAt())
		if err != nil {
			return reminderPlan{}, err
		}
		return reminderPlan{Kind: "at", Schedule: req.GetFireAt(), NextRunUnix: nextRun, RecurrenceTimezone: req.GetTimezone()}, nil
	}
	if schedule := req.GetRecurring(); schedule != nil {
		kind := reminderScheduleKindToStorage(schedule.GetKind())
		if kind == "" {
			return reminderPlan{}, errors.New("recurring schedule kind is required")
		}
		return reminderPlan{
			Kind:                  kind,
			Schedule:              schedule.GetExpression(),
			NextRunUnix:           0,
			RecurrenceRule:        schedule.GetExpression(),
			RecurrenceDescription: schedule.GetExpression(),
			RecurrenceTimezone:    firstNonEmpty(schedule.GetTimezone(), req.GetTimezone()),
		}, nil
	}
	return reminderPlan{}, errors.New("schedule is required")
}

func reminderPatchFromUpdateRequest(req *daemonv1.UpdateReminderRequest) (storage.ReminderPatch, error) {
	patch := storage.ReminderPatch{}
	if req.Title != nil {
		value := strings.TrimSpace(req.GetTitle())
		patch.Title = &value
	}
	if req.Timezone != nil {
		value := strings.TrimSpace(req.GetTimezone())
		patch.RecurrenceTimezone = &value
	}
	if req.GetDelaySeconds() > 0 {
		kind := "at"
		schedule := fmt.Sprintf("in %ds", req.GetDelaySeconds())
		nextRun := time.Now().Add(time.Duration(req.GetDelaySeconds()) * time.Second).Unix()
		patch.ScheduleKind = &kind
		patch.Schedule = &schedule
		patch.NextRunUnix = &nextRun
		return patch, nil
	}
	if req.GetFireAt() != "" {
		nextRun, err := parseReminderFireAt(req.GetFireAt())
		if err != nil {
			return storage.ReminderPatch{}, err
		}
		kind := "at"
		schedule := req.GetFireAt()
		patch.ScheduleKind = &kind
		patch.Schedule = &schedule
		patch.NextRunUnix = &nextRun
		return patch, nil
	}
	if schedule := req.GetRecurring(); schedule != nil {
		kind := reminderScheduleKindToStorage(schedule.GetKind())
		if kind == "" {
			return storage.ReminderPatch{}, errors.New("recurring schedule kind is required")
		}
		expression := schedule.GetExpression()
		timezone := firstNonEmpty(schedule.GetTimezone(), req.GetTimezone())
		patch.ScheduleKind = &kind
		patch.Schedule = &expression
		patch.RecurrenceRule = &expression
		patch.RecurrenceDescription = &expression
		patch.RecurrenceTimezone = &timezone
	}
	return patch, nil
}

func parseReminderFireAt(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("fire_at is required")
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > 0 {
		return unix, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return parsed.Unix(), nil
		}
	}
	return 0, errors.New("fire_at must be unix seconds or RFC3339 time")
}

func reminderActorFromContext(ctx *daemonv1.RequestContext) (string, string) {
	actor := ctx.GetActor()
	switch actor.GetActorKind() {
	case daemonv1.ActorKind_ACTOR_KIND_HUMAN:
		return "human", actor.GetUserId()
	case daemonv1.ActorKind_ACTOR_KIND_AGENT:
		return "agent", actor.GetAgentId()
	case daemonv1.ActorKind_ACTOR_KIND_SYSTEM, daemonv1.ActorKind_ACTOR_KIND_DAEMON:
		return "system", firstNonEmpty(actor.GetDaemonId(), actor.GetDisplayName())
	default:
		return "system", ""
	}
}

func reminderStatusToStorage(status daemonv1.ReminderStatus) string {
	switch status {
	case daemonv1.ReminderStatus_REMINDER_STATUS_ACTIVE:
		return "active"
	case daemonv1.ReminderStatus_REMINDER_STATUS_DONE:
		return "done"
	case daemonv1.ReminderStatus_REMINDER_STATUS_CANCELED:
		return "canceled"
	case daemonv1.ReminderStatus_REMINDER_STATUS_PAUSED:
		return "paused"
	case daemonv1.ReminderStatus_REMINDER_STATUS_FAILED:
		return "failed"
	default:
		return ""
	}
}

func reminderStatusFromStorage(status string) daemonv1.ReminderStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return daemonv1.ReminderStatus_REMINDER_STATUS_ACTIVE
	case "done":
		return daemonv1.ReminderStatus_REMINDER_STATUS_DONE
	case "canceled", "cancelled":
		return daemonv1.ReminderStatus_REMINDER_STATUS_CANCELED
	case "paused":
		return daemonv1.ReminderStatus_REMINDER_STATUS_PAUSED
	case "failed":
		return daemonv1.ReminderStatus_REMINDER_STATUS_FAILED
	default:
		return daemonv1.ReminderStatus_REMINDER_STATUS_UNSPECIFIED
	}
}

func reminderScheduleKindToStorage(kind daemonv1.ReminderScheduleKind) string {
	switch kind {
	case daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_CRON:
		return "cron"
	case daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_EVERY:
		return "every"
	case daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_AT:
		return "at"
	case daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_RRULE:
		return "rrule"
	case daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_NATURAL:
		return "natural"
	default:
		return ""
	}
}

func reminderScheduleKindFromStorage(kind string) daemonv1.ReminderScheduleKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "cron":
		return daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_CRON
	case "every":
		return daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_EVERY
	case "at":
		return daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_AT
	case "rrule":
		return daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_RRULE
	case "natural":
		return daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_NATURAL
	default:
		return daemonv1.ReminderScheduleKind_REMINDER_SCHEDULE_KIND_UNSPECIFIED
	}
}

func reminderEventTypeFromStorage(value string) daemonv1.ReminderEventType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "created":
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_CREATED
	case "fired":
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_FIRED
	case "snoozed":
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_SNOOZED
	case "updated":
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_UPDATED
	case "canceled", "cancelled":
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_CANCELED
	case "failed":
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_FAILED
	default:
		return daemonv1.ReminderEventType_REMINDER_EVENT_TYPE_UNSPECIFIED
	}
}

func reminderActorTypeFromStorage(value string) daemonv1.ReminderActorType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "human":
		return daemonv1.ReminderActorType_REMINDER_ACTOR_TYPE_HUMAN
	case "agent":
		return daemonv1.ReminderActorType_REMINDER_ACTOR_TYPE_AGENT
	case "system":
		return daemonv1.ReminderActorType_REMINDER_ACTOR_TYPE_SYSTEM
	default:
		return daemonv1.ReminderActorType_REMINDER_ACTOR_TYPE_UNSPECIFIED
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

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
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
