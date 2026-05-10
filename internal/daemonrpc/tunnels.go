package daemonrpc

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"github.com/ca-x/nekode/internal/tunnelregistry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	tunnelDefaultTTLSeconds uint32 = 2 * 60 * 60
	tunnelMaxTTLSeconds     uint32 = 24 * 60 * 60
)

// CreateTunnel is the agent-facing entry point. Agent-initiated tunnels
// land in PENDING_APPROVAL so a human admin has to release them; humans
// calling the REST API get an ACTIVE tunnel immediately, per design.
func (s *Server) CreateTunnel(ctx context.Context, req *daemonv1.CreateTunnelRequest) (*daemonv1.CreateTunnelResponse, error) {
	computerID := strings.TrimSpace(req.GetComputerId())
	if computerID == "" || req.GetLocalPort() == 0 {
		return nil, status.Error(codes.InvalidArgument, "computer_id and local_port are required")
	}
	ttl := req.GetTtlSeconds()
	if ttl == 0 {
		ttl = tunnelDefaultTTLSeconds
	}
	if ttl > tunnelMaxTTLSeconds {
		ttl = tunnelMaxTTLSeconds
	}
	policy := accessPolicyToStorage(req.GetAccessPolicy())
	now := time.Now().Unix()
	record, err := s.store.CreateTunnel(ctx, storage.TunnelRecord{
		ComputerID:   computerID,
		LocalPort:    req.GetLocalPort(),
		Label:        strings.TrimSpace(req.GetLabel()),
		State:        storage.TunnelStatePending,
		AccessPolicy: policy,
		CreatorID:    initiatorFromContext(req.GetContext()),
		CreatorKind:  "agent",
		ExpiresUnix:  now + int64(ttl),
	})
	if errors.Is(err, storage.ErrInvalid) {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create tunnel: %v", err)
	}
	return &daemonv1.CreateTunnelResponse{Tunnel: tunnelToProto(record)}, nil
}

// ListTunnels mirrors the REST listing with a proto shape. Tokens are
// stripped for the same reason as the REST path.
func (s *Server) ListTunnels(ctx context.Context, req *daemonv1.ListTunnelsRequest) (*daemonv1.ListTunnelsResponse, error) {
	opts := storage.TunnelListOptions{
		ComputerID:  strings.TrimSpace(req.GetComputerId()),
		StateFilter: tunnelStateToStorage(req.GetStateFilter()),
		Limit:       int(req.GetLimit()),
	}
	rows, err := s.store.ListTunnels(ctx, opts)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tunnels: %v", err)
	}
	out := make([]*daemonv1.Tunnel, 0, len(rows))
	for _, row := range rows {
		row.Token = ""
		out = append(out, tunnelToProto(row))
	}
	return &daemonv1.ListTunnelsResponse{Tunnels: out}, nil
}

// ApproveTunnel flips pending → active.
func (s *Server) ApproveTunnel(ctx context.Context, req *daemonv1.ApproveTunnelRequest) (*daemonv1.ApproveTunnelResponse, error) {
	id := strings.TrimSpace(req.GetTunnelId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "tunnel_id is required")
	}
	actor := initiatorFromContext(req.GetContext())
	updated, err := s.store.UpdateTunnelState(ctx, id, storage.TunnelStateActive, actor, "")
	if err != nil {
		return nil, tunnelStateErr(err)
	}
	updated.Token = ""
	return &daemonv1.ApproveTunnelResponse{Tunnel: tunnelToProto(updated)}, nil
}

// RejectTunnel marks pending → rejected with an optional reason.
func (s *Server) RejectTunnel(ctx context.Context, req *daemonv1.RejectTunnelRequest) (*daemonv1.RejectTunnelResponse, error) {
	id := strings.TrimSpace(req.GetTunnelId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "tunnel_id is required")
	}
	actor := initiatorFromContext(req.GetContext())
	updated, err := s.store.UpdateTunnelState(ctx, id, storage.TunnelStateRejected, actor, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, tunnelStateErr(err)
	}
	updated.Token = ""
	return &daemonv1.RejectTunnelResponse{Tunnel: tunnelToProto(updated)}, nil
}

// CloseTunnel is the terminal transition; anyone holding a reference (the
// creator, the daemon on disconnect, the TTL sweeper) can land here.
func (s *Server) CloseTunnel(ctx context.Context, req *daemonv1.CloseTunnelRequest) (*daemonv1.CloseTunnelResponse, error) {
	id := strings.TrimSpace(req.GetTunnelId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "tunnel_id is required")
	}
	actor := initiatorFromContext(req.GetContext())
	updated, err := s.store.UpdateTunnelState(ctx, id, storage.TunnelStateClosed, actor, strings.TrimSpace(req.GetReason()))
	if err != nil {
		return nil, tunnelStateErr(err)
	}
	updated.Token = ""
	return &daemonv1.CloseTunnelResponse{Tunnel: tunnelToProto(updated)}, nil
}

func tunnelStateErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return status.Error(codes.NotFound, "tunnel not found")
	case errors.Is(err, storage.ErrInvalid):
		return status.Error(codes.FailedPrecondition, "invalid tunnel state transition")
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}

// ProxyExchange is the long-lived bidi stream per daemon. The very first
// frame the daemon sends MUST carry tunnel_id == "__attach__" and
// request_id == its computer_id so the server can bind the stream to
// the right computer. All subsequent frames are RESPONSE_* or CANCEL
// frames correlated by request_id.
func (s *Server) ProxyExchange(stream daemonv1.DaemonControlService_ProxyExchangeServer) error {
	if s.tunnels == nil {
		return status.Error(codes.Unavailable, "tunnel registry not configured on this server")
	}
	attach, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "expected attach frame: %v", err)
	}
	if attach.GetTunnelId() != "__attach__" {
		return status.Error(codes.InvalidArgument, "first frame must be an attach frame (tunnel_id=__attach__)")
	}
	computerID := attach.GetRequestId()
	if computerID == "" {
		return status.Error(codes.InvalidArgument, "attach frame missing computer id in request_id")
	}
	s.tunnels.Attach(computerID, stream)
	defer s.tunnels.Detach(computerID, stream)
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if dispatchErr := s.tunnels.Dispatch(computerID, frame); dispatchErr != nil {
			if errors.Is(dispatchErr, tunnelregistry.ErrRequestGone) {
				continue
			}
			return dispatchErr
		}
	}
}

func tunnelStateToStorage(state daemonv1.TunnelState) string {
	switch state {
	case daemonv1.TunnelState_TUNNEL_STATE_PENDING_APPROVAL:
		return storage.TunnelStatePending
	case daemonv1.TunnelState_TUNNEL_STATE_ACTIVE:
		return storage.TunnelStateActive
	case daemonv1.TunnelState_TUNNEL_STATE_REJECTED:
		return storage.TunnelStateRejected
	case daemonv1.TunnelState_TUNNEL_STATE_CLOSED:
		return storage.TunnelStateClosed
	default:
		return ""
	}
}

func tunnelStateFromStorage(state string) daemonv1.TunnelState {
	switch state {
	case storage.TunnelStatePending:
		return daemonv1.TunnelState_TUNNEL_STATE_PENDING_APPROVAL
	case storage.TunnelStateActive:
		return daemonv1.TunnelState_TUNNEL_STATE_ACTIVE
	case storage.TunnelStateRejected:
		return daemonv1.TunnelState_TUNNEL_STATE_REJECTED
	case storage.TunnelStateClosed:
		return daemonv1.TunnelState_TUNNEL_STATE_CLOSED
	default:
		return daemonv1.TunnelState_TUNNEL_STATE_UNSPECIFIED
	}
}

func accessPolicyToStorage(policy daemonv1.TunnelAccessPolicy) string {
	switch policy {
	case daemonv1.TunnelAccessPolicy_TUNNEL_ACCESS_POLICY_PRIVATE:
		return storage.TunnelAccessPolicyPrivate
	case daemonv1.TunnelAccessPolicy_TUNNEL_ACCESS_POLICY_PUBLIC:
		return storage.TunnelAccessPolicyPublic
	default:
		return storage.TunnelAccessPolicyMembers
	}
}

func accessPolicyFromStorage(policy string) daemonv1.TunnelAccessPolicy {
	switch policy {
	case storage.TunnelAccessPolicyPrivate:
		return daemonv1.TunnelAccessPolicy_TUNNEL_ACCESS_POLICY_PRIVATE
	case storage.TunnelAccessPolicyPublic:
		return daemonv1.TunnelAccessPolicy_TUNNEL_ACCESS_POLICY_PUBLIC
	case storage.TunnelAccessPolicyMembers:
		return daemonv1.TunnelAccessPolicy_TUNNEL_ACCESS_POLICY_MEMBERS
	default:
		return daemonv1.TunnelAccessPolicy_TUNNEL_ACCESS_POLICY_UNSPECIFIED
	}
}

// initiatorFromContext extracts the stable actor identifier from a
// RequestContext. Agents prefer agent_id, humans prefer user_id; either
// way we want one string to stamp as creator/approver/closer.
func initiatorFromContext(rc *daemonv1.RequestContext) string {
	actor := rc.GetActor()
	if actor == nil {
		return ""
	}
	if id := strings.TrimSpace(actor.GetAgentId()); id != "" {
		return id
	}
	if id := strings.TrimSpace(actor.GetUserId()); id != "" {
		return id
	}
	return strings.TrimSpace(actor.GetDaemonId())
}

func creatorKindToProto(kind string) daemonv1.ActorKind {
	switch kind {
	case "agent":
		return daemonv1.ActorKind_ACTOR_KIND_AGENT
	case "human":
		return daemonv1.ActorKind_ACTOR_KIND_HUMAN
	default:
		return daemonv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

func tunnelToProto(record storage.TunnelRecord) *daemonv1.Tunnel {
	return &daemonv1.Tunnel{
		TunnelId:         record.ID,
		Token:            record.Token,
		ComputerId:       record.ComputerID,
		DaemonId:         record.DaemonID,
		LocalPort:        record.LocalPort,
		Label:            record.Label,
		PublicUrl:        record.PublicURL,
		State:            tunnelStateFromStorage(record.State),
		AccessPolicy:     accessPolicyFromStorage(record.AccessPolicy),
		CreatorId:        record.CreatorID,
		CreatorKind:      creatorKindToProto(record.CreatorKind),
		CreatedTimeUnix:  record.CreatedUnix,
		ExpiresTimeUnix:  record.ExpiresUnix,
		ApprovedTimeUnix: record.ApprovedUnix,
		ApprovedBy:       record.ApprovedBy,
		ClosedTimeUnix:   record.ClosedUnix,
		CloseReason:      record.CloseReason,
	}
}
