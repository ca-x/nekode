package storage

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/ca-x/nekode/internal/ent"
	"github.com/ca-x/nekode/internal/ent/tunnel"
)

// Tunnel lifecycle states.
const (
	TunnelStatePending  = "pending_approval"
	TunnelStateActive   = "active"
	TunnelStateRejected = "rejected"
	TunnelStateClosed   = "closed"
)

// Tunnel access policies.
const (
	TunnelAccessPolicyPrivate = "private"
	TunnelAccessPolicyMembers = "members"
	TunnelAccessPolicyPublic  = "public"
)

var validTunnelStates = map[string]struct{}{
	TunnelStatePending:  {},
	TunnelStateActive:   {},
	TunnelStateRejected: {},
	TunnelStateClosed:   {},
}

var validTunnelAccessPolicies = map[string]struct{}{
	TunnelAccessPolicyPrivate: {},
	TunnelAccessPolicyMembers: {},
	TunnelAccessPolicyPublic:  {},
}

// TunnelRecord mirrors the ent row in a layer-neutral shape.
type TunnelRecord struct {
	ID           string `json:"id"`
	Token        string `json:"token"`
	ComputerID   string `json:"computerId"`
	DaemonID     string `json:"daemonId"`
	LocalPort    uint32 `json:"localPort"`
	Label        string `json:"label"`
	State        string `json:"state"`
	AccessPolicy string `json:"accessPolicy"`
	CreatorID    string `json:"creatorId"`
	CreatorKind  string `json:"creatorKind"`
	CreatedUnix  int64  `json:"createdUnix"`
	ExpiresUnix  int64  `json:"expiresUnix"`
	ApprovedUnix int64  `json:"approvedUnix,omitempty"`
	ApprovedBy   string `json:"approvedBy,omitempty"`
	ClosedUnix   int64  `json:"closedUnix,omitempty"`
	CloseReason  string `json:"closeReason,omitempty"`
	// PublicURL is derived from the inbound request host and only populated
	// on response-path; it is not persisted. Empty on list responses where
	// the token has been stripped.
	PublicURL string `json:"publicUrl,omitempty"`
}

// TunnelListOptions filters tunnel listings.
type TunnelListOptions struct {
	ComputerID  string
	StateFilter string
	Limit       int
}

// NewTunnelToken returns a URL-safe, 32-byte random token (256 bits of
// entropy). Used verbatim in the public URL; treat it like a session
// secret. Generator uses crypto/rand; failure is surfaced to the caller
// because a weak token would be a silent security regression.
func NewTunnelToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CreateTunnel inserts a new tunnel row.
func (s *Store) CreateTunnel(ctx context.Context, record TunnelRecord) (TunnelRecord, error) {
	state := strings.TrimSpace(record.State)
	if _, ok := validTunnelStates[state]; !ok {
		return TunnelRecord{}, ErrInvalid
	}
	policy := strings.TrimSpace(record.AccessPolicy)
	if _, ok := validTunnelAccessPolicies[policy]; !ok {
		return TunnelRecord{}, ErrInvalid
	}
	if record.CreatedUnix == 0 {
		record.CreatedUnix = unixNow()
	}
	if record.Token == "" {
		token, err := NewTunnelToken()
		if err != nil {
			return TunnelRecord{}, err
		}
		record.Token = token
	}
	create := s.client.Tunnel.Create().
		SetToken(record.Token).
		SetComputerID(record.ComputerID).
		SetDaemonID(record.DaemonID).
		SetLocalPort(record.LocalPort).
		SetLabel(record.Label).
		SetState(state).
		SetAccessPolicy(policy).
		SetCreatorID(record.CreatorID).
		SetCreatorKind(record.CreatorKind).
		SetCreatedUnix(record.CreatedUnix).
		SetExpiresUnix(record.ExpiresUnix).
		SetApprovedUnix(record.ApprovedUnix).
		SetApprovedBy(record.ApprovedBy).
		SetClosedUnix(record.ClosedUnix).
		SetCloseReason(record.CloseReason)
	if record.ID != "" {
		create.SetID(record.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return TunnelRecord{}, ErrConflict
	}
	if err != nil {
		return TunnelRecord{}, err
	}
	return tunnelFromEnt(row), nil
}

// GetTunnel returns a tunnel by id.
func (s *Store) GetTunnel(ctx context.Context, id string) (TunnelRecord, error) {
	row, err := s.client.Tunnel.Get(ctx, id)
	if ent.IsNotFound(err) {
		return TunnelRecord{}, ErrNotFound
	}
	if err != nil {
		return TunnelRecord{}, err
	}
	return tunnelFromEnt(row), nil
}

// GetTunnelByToken is the hot-path lookup the reverse proxy uses for
// every inbound /preview/<token>/ request. Tokens are indexed unique.
func (s *Store) GetTunnelByToken(ctx context.Context, token string) (TunnelRecord, error) {
	row, err := s.client.Tunnel.Query().Where(tunnel.TokenEQ(token)).Only(ctx)
	if ent.IsNotFound(err) {
		return TunnelRecord{}, ErrNotFound
	}
	if err != nil {
		return TunnelRecord{}, err
	}
	return tunnelFromEnt(row), nil
}

// ListTunnels applies standard filters; rows come back newest-first.
func (s *Store) ListTunnels(ctx context.Context, opts TunnelListOptions) ([]TunnelRecord, error) {
	query := s.client.Tunnel.Query()
	if opts.ComputerID != "" {
		query.Where(tunnel.ComputerIDEQ(opts.ComputerID))
	}
	if opts.StateFilter != "" {
		query.Where(tunnel.StateEQ(opts.StateFilter))
	}
	query.Order(ent.Desc(tunnel.FieldCreatedUnix))
	if opts.Limit > 0 {
		query.Limit(opts.Limit)
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TunnelRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, tunnelFromEnt(row))
	}
	return out, nil
}

// UpdateTunnelState transitions a tunnel through its lifecycle. Valid
// transitions:
//   pending_approval → active   (ApproveTunnel)
//   pending_approval → rejected (RejectTunnel)
//   active            → closed  (CloseTunnel / daemon disconnect / TTL)
//   pending_approval → closed   (operator cancels own request)
// Any other transition returns ErrInvalid.
func (s *Store) UpdateTunnelState(
	ctx context.Context,
	id, toState, actor, reason string,
) (TunnelRecord, error) {
	if _, ok := validTunnelStates[toState]; !ok {
		return TunnelRecord{}, ErrInvalid
	}
	tx, err := s.client.BeginTx(ctx, nil)
	if err != nil {
		return TunnelRecord{}, err
	}
	// Tx lifecycle: we commit at the happy-path bottom, and roll back on
	// every early return. Historical versions of this function relied on
	// a named-return / defer pattern that silently skipped rollback for
	// ErrInvalid paths — that pinned SQLite connections on repeat calls.
	// Explicit rollback at every error branch is uglier but correct.
	row, err := tx.Tunnel.Get(ctx, id)
	if ent.IsNotFound(err) {
		_ = tx.Rollback()
		return TunnelRecord{}, ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback()
		return TunnelRecord{}, err
	}
	if !validTunnelTransition(row.State, toState) {
		_ = tx.Rollback()
		return TunnelRecord{}, ErrInvalid
	}
	now := unixNow()
	update := tx.Tunnel.UpdateOneID(id).SetState(toState)
	switch toState {
	case TunnelStateActive:
		update.SetApprovedUnix(now).SetApprovedBy(actor)
	case TunnelStateRejected, TunnelStateClosed:
		update.SetClosedUnix(now).SetCloseReason(reason)
	}
	updated, err := update.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		return TunnelRecord{}, err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return TunnelRecord{}, commitErr
	}
	return tunnelFromEnt(updated), nil
}

// CloseExpiredTunnels marks every active/pending tunnel whose expiry has
// passed as closed. Called from the registry's ticker.
func (s *Store) CloseExpiredTunnels(ctx context.Context, now int64) (int, error) {
	if now <= 0 {
		now = unixNow()
	}
	rows, err := s.client.Tunnel.Query().
		Where(
			tunnel.StateIn(TunnelStatePending, TunnelStateActive),
			tunnel.ExpiresUnixGT(0),
			tunnel.ExpiresUnixLTE(now),
		).All(ctx)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if _, err := s.client.Tunnel.UpdateOneID(row.ID).
			SetState(TunnelStateClosed).
			SetClosedUnix(now).
			SetCloseReason("ttl_expired").
			Save(ctx); err != nil && !ent.IsNotFound(err) {
			return 0, err
		}
	}
	return len(rows), nil
}

// CloseTunnelsForComputer bulk-closes tunnels when a daemon disconnects.
// Not a lifecycle operation the UI can trigger — called from the server
// when it observes daemon heartbeat gaps past threshold.
func (s *Store) CloseTunnelsForComputer(ctx context.Context, computerID, reason string) error {
	if computerID == "" {
		return errors.New("computer id is required")
	}
	now := unixNow()
	_, err := s.client.Tunnel.Update().
		Where(
			tunnel.ComputerIDEQ(computerID),
			tunnel.StateIn(TunnelStatePending, TunnelStateActive),
		).
		SetState(TunnelStateClosed).
		SetClosedUnix(now).
		SetCloseReason(reason).
		Save(ctx)
	return err
}

// validTunnelTransition implements the lifecycle rules above.
func validTunnelTransition(from, to string) bool {
	switch from {
	case TunnelStatePending:
		return to == TunnelStateActive || to == TunnelStateRejected || to == TunnelStateClosed
	case TunnelStateActive:
		return to == TunnelStateClosed
	default:
		return false
	}
}

func tunnelFromEnt(row *ent.Tunnel) TunnelRecord {
	return TunnelRecord{
		ID:           row.ID,
		Token:        row.Token,
		ComputerID:   row.ComputerID,
		DaemonID:     row.DaemonID,
		LocalPort:    row.LocalPort,
		Label:        row.Label,
		State:        row.State,
		AccessPolicy: row.AccessPolicy,
		CreatorID:    row.CreatorID,
		CreatorKind:  row.CreatorKind,
		CreatedUnix:  row.CreatedUnix,
		ExpiresUnix:  row.ExpiresUnix,
		ApprovedUnix: row.ApprovedUnix,
		ApprovedBy:   row.ApprovedBy,
		ClosedUnix:   row.ClosedUnix,
		CloseReason:  row.CloseReason,
	}
}
