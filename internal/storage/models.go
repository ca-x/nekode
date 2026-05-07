package storage

import "errors"

var (
	ErrConflict     = errors.New("conflict")
	ErrInvalidState = errors.New("invalid state")
	ErrNotFound     = errors.New("not found")
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"displayName"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	CreatedUnix  int64  `json:"createdUnix"`
	UpdatedUnix  int64  `json:"updatedUnix"`
}

type Session struct {
	ID          string `json:"id"`
	TokenHash   string `json:"-"`
	UserID      string `json:"userId"`
	ExpiresUnix int64  `json:"expiresUnix"`
	CreatedUnix int64  `json:"createdUnix"`
}

type InteractionEndpoint struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Provider        string `json:"provider"`
	DisplayName     string `json:"displayName"`
	TargetPrefix    string `json:"targetPrefix"`
	InboundEnabled  bool   `json:"inboundEnabled"`
	OutboundEnabled bool   `json:"outboundEnabled"`
	AuthMode        string `json:"authMode"`
	ConfigJSON      string `json:"configJson"`
	CreatedUnix     int64  `json:"createdUnix"`
	UpdatedUnix     int64  `json:"updatedUnix"`
}

type Message struct {
	ID                string `json:"id"`
	Target            string `json:"target"`
	ThreadID          string `json:"threadId,omitempty"`
	Role              string `json:"role"`
	Content           string `json:"content"`
	SenderUserID      string `json:"senderUserId,omitempty"`
	SenderAgentID     string `json:"senderAgentId,omitempty"`
	SenderDisplayName string `json:"senderDisplayName,omitempty"`
	SenderKind        string `json:"senderKind"`
	SourceEndpointID  string `json:"sourceEndpointId,omitempty"`
	ExternalMessageID string `json:"externalMessageId,omitempty"`
	MetadataJSON      string `json:"metadataJson,omitempty"`
	RequestID         string `json:"requestId,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
}

type Task struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	State           string `json:"state"`
	Target          string `json:"target"`
	AssigneeID      string `json:"assigneeId,omitempty"`
	CreatedByUserID string `json:"createdByUserId,omitempty"`
	CreatedUnix     int64  `json:"createdUnix"`
	UpdatedUnix     int64  `json:"updatedUnix"`
}

type TaskPatch struct {
	Summary    *string
	State      *string
	AssigneeID *string
}
