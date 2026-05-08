package storage

import "errors"

var (
	ErrConflict     = errors.New("conflict")
	ErrInProgress   = errors.New("in progress")
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
	ID                string       `json:"id"`
	Target            string       `json:"target"`
	ThreadID          string       `json:"threadId,omitempty"`
	Role              string       `json:"role"`
	Content           string       `json:"content"`
	SenderUserID      string       `json:"senderUserId,omitempty"`
	SenderAgentID     string       `json:"senderAgentId,omitempty"`
	SenderDisplayName string       `json:"senderDisplayName,omitempty"`
	SenderKind        string       `json:"senderKind"`
	SourceEndpointID  string       `json:"sourceEndpointId,omitempty"`
	ExternalMessageID string       `json:"externalMessageId,omitempty"`
	MetadataJSON      string       `json:"metadataJson,omitempty"`
	Attachments       []Attachment `json:"attachments,omitempty"`
	RequestID         string       `json:"requestId,omitempty"`
	CreatedUnix       int64        `json:"createdUnix"`
}

type Attachment struct {
	ID              string `json:"id"`
	Target          string `json:"target"`
	OwnerID         string `json:"ownerId,omitempty"`
	Filename        string `json:"filename"`
	MimeType        string `json:"mimeType"`
	SizeBytes       int64  `json:"sizeBytes"`
	StorageRef      string `json:"storageRef"`
	DownloadURL     string `json:"downloadUrl"`
	UploadURL       string `json:"uploadUrl,omitempty"`
	ExpiresTimeUnix int64  `json:"expiresTimeUnix,omitempty"`
	CreatedUnix     int64  `json:"createdUnix"`
}

type ThreadInboxItem struct {
	Target            string  `json:"target"`
	ThreadID          string  `json:"threadId"`
	Topic             string  `json:"topic"`
	MessageCount      int     `json:"messageCount"`
	UnreadCount       int     `json:"unreadCount"`
	LastReadUnix      int64   `json:"lastReadUnix"`
	LastReadMessageID string  `json:"lastReadMessageId,omitempty"`
	FirstMessage      Message `json:"firstMessage"`
	LatestMessage     Message `json:"latestMessage"`
	UpdatedUnix       int64   `json:"updatedUnix"`
}

type Task struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	Description     string `json:"description"`
	State           string `json:"state"`
	Target          string `json:"target"`
	AssigneeID      string `json:"assigneeId,omitempty"`
	CreatedByUserID string `json:"createdByUserId,omitempty"`
	BlockedReason   string `json:"blockedReason,omitempty"`
	Version         int64  `json:"version"`
	ClaimLeaseID    string `json:"claimLeaseId,omitempty"`
	CreatedUnix     int64  `json:"createdUnix"`
	UpdatedUnix     int64  `json:"updatedUnix"`
}

type TaskPatch struct {
	Summary       *string
	Description   *string
	State         *string
	AssigneeID    *string
	BlockedReason *string
}

type CollaborationEvent struct {
	ID              string `json:"id"`
	ServerID        string `json:"serverId"`
	Sequence        int64  `json:"sequence"`
	EventID         string `json:"eventId"`
	Target          string `json:"target"`
	AggregateID     string `json:"aggregateId"`
	Kind            string `json:"kind"`
	Operation       string `json:"operation,omitempty"`
	ScopeType       string `json:"scopeType,omitempty"`
	ScopeID         string `json:"scopeId,omitempty"`
	WorkspaceID     string `json:"workspaceId,omitempty"`
	ActivityID      string `json:"activityId,omitempty"`
	PayloadJSON     string `json:"payloadJson"`
	CreatedUnix     int64  `json:"createdUnix"`
	ProtocolVersion int    `json:"protocolVersion"`
}

type IdempotencyRecord struct {
	ID             string `json:"id"`
	Scope          string `json:"scope"`
	Method         string `json:"method"`
	ActorID        string `json:"actorId"`
	IdempotencyKey string `json:"idempotencyKey"`
	RequestHash    string `json:"requestHash"`
	ResponseType   string `json:"responseType"`
	ResponseJSON   string `json:"responseJson"`
	ResourceType   string `json:"resourceType"`
	ResourceID     string `json:"resourceId"`
	Status         string `json:"status"`
	CreatedUnix    int64  `json:"createdUnix"`
	ExpiresUnix    int64  `json:"expiresUnix"`
}
