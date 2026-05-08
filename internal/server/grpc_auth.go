package server

import (
	"context"
	"strings"

	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const daemonEnrollmentKey contextKey = "daemonEnrollment"

func (s *Server) grpcServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.UnaryInterceptor(s.requireDaemonTokenUnary),
		grpc.StreamInterceptor(s.requireDaemonTokenStream),
	}
}

func (s *Server) requireDaemonTokenUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	enrollment, err := s.authenticateDaemonGRPC(ctx)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, daemonEnrollmentKey, enrollment)
	resp, err := handler(ctx, req)
	if err == nil {
		s.markDaemonEnrollmentConnected(enrollment.ID, req)
	}
	return resp, err
}

func (s *Server) requireDaemonTokenStream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if _, err := s.authenticateDaemonGRPC(stream.Context()); err != nil {
		return err
	}
	return handler(srv, stream)
}

func (s *Server) authenticateDaemonGRPC(ctx context.Context) (daemonEnrollment, error) {
	if s.daemonEnrollments == nil {
		return daemonEnrollment{}, status.Error(codes.Unauthenticated, "daemon enrollment is not configured")
	}
	got := bearerTokenFromGRPC(ctx)
	enrollment, err := s.daemonEnrollments.authenticate(got)
	if err != nil {
		if err == storage.ErrNotFound {
			return daemonEnrollment{}, status.Error(codes.Unauthenticated, "invalid daemon enrollment token")
		}
		return daemonEnrollment{}, status.Error(codes.Unauthenticated, "daemon enrollment unavailable")
	}
	return enrollment, nil
}

func (s *Server) markDaemonEnrollmentConnected(enrollmentID string, req any) {
	if s.daemonEnrollments == nil || strings.TrimSpace(enrollmentID) == "" {
		return
	}
	switch typed := req.(type) {
	case *daemonv1.RegisterComputerRequest:
		if err := s.daemonEnrollments.markConnected(enrollmentID, typed.GetInfo(), true); err != nil {
			s.logger.Warn("failed to mark daemon enrollment connected", "enrollment", enrollmentID, "error", err)
		}
	case *daemonv1.HeartbeatComputerRequest:
		if err := s.daemonEnrollments.markConnected(enrollmentID, typed.GetInfo(), true); err != nil {
			s.logger.Warn("failed to mark daemon enrollment heartbeat", "enrollment", enrollmentID, "error", err)
		}
	case *daemonv1.SyncComputerInventoryRequest:
		if err := s.daemonEnrollments.markConnected(enrollmentID, typed.GetInfo(), false); err != nil {
			s.logger.Warn("failed to mark daemon enrollment inventory sync", "enrollment", enrollmentID, "error", err)
		}
	}
}

func bearerTokenFromGRPC(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get("authorization") {
		scheme, token, ok := strings.Cut(strings.TrimSpace(value), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token)
		}
	}
	return ""
}
