package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	daemonv1 "github.com/ca-x/nekode/gen/go/nekode/daemon/v1"
	"github.com/ca-x/nekode/internal/storage"
)

type daemonAuthInterceptor struct {
	server *Server
}

type daemonEnrollmentContextKey struct{}

func (s *Server) daemonAuthInterceptor() connect.Interceptor {
	return daemonAuthInterceptor{server: s}
}

func (i daemonAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		enrollment, err := i.server.authenticateDaemonConnect(req.Header())
		if err != nil {
			return nil, err
		}
		ctx = context.WithValue(ctx, daemonEnrollmentContextKey{}, enrollment)
		resp, err := next(ctx, req)
		if err == nil {
			i.server.markDaemonEnrollmentConnected(enrollment.ID, req.Any())
		}
		return resp, err
	}
}

func (i daemonAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i daemonAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		enrollment, err := i.server.authenticateDaemonConnect(conn.RequestHeader())
		if err != nil {
			return err
		}
		ctx = context.WithValue(ctx, daemonEnrollmentContextKey{}, enrollment)
		i.server.markDaemonEnrollmentConnected(enrollment.ID, nil)
		return next(ctx, conn)
	}
}

func (s *Server) authenticateDaemonConnect(header http.Header) (daemonEnrollment, error) {
	if s.daemonEnrollments == nil {
		return daemonEnrollment{}, connect.NewError(connect.CodeUnauthenticated, errors.New("daemon enrollment is not configured"))
	}
	got := bearerTokenFromHeader(header)
	enrollment, err := s.daemonEnrollments.authenticate(got)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return daemonEnrollment{}, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid daemon enrollment token"))
		}
		return daemonEnrollment{}, connect.NewError(connect.CodeUnauthenticated, errors.New("daemon enrollment unavailable"))
	}
	return enrollment, nil
}

func (s *Server) markDaemonEnrollmentConnected(enrollmentID string, req any) {
	if s.daemonEnrollments == nil || strings.TrimSpace(enrollmentID) == "" {
		return
	}
	switch typed := req.(type) {
	case nil:
		if err := s.daemonEnrollments.markConnected(enrollmentID, nil, false); err != nil {
			s.logger.Warn("failed to mark daemon enrollment stream connected", "enrollment", enrollmentID, "error", err)
		}
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

func bearerTokenFromHeader(header http.Header) string {
	for _, value := range header.Values("Authorization") {
		scheme, token, ok := strings.Cut(strings.TrimSpace(value), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token)
		}
	}
	return ""
}
