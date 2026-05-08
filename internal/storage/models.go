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

type InteractionEndpointPatch struct {
	DisplayName     *string
	TargetPrefix    *string
	InboundEnabled  *bool
	OutboundEnabled *bool
	AuthMode        *string
	ConfigJSON      *string
}

type ChannelSummary struct {
	Target          string `json:"target"`
	DisplayName     string `json:"displayName"`
	ChannelType     string `json:"channelType"`
	Visibility      string `json:"visibility"`
	Joined          bool   `json:"joined"`
	MemberCount     int    `json:"memberCount"`
	CurrentUserRole string `json:"currentUserRole,omitempty"`
}

type ChannelMember struct {
	Target         string `json:"target"`
	MemberID       string `json:"memberId"`
	Username       string `json:"username,omitempty"`
	DisplayName    string `json:"displayName"`
	Kind           string `json:"kind"`
	Role           string `json:"role"`
	JoinedTimeUnix int64  `json:"joinedTimeUnix"`
}

type Message struct {
	ID                string       `json:"id"`
	Target            string       `json:"target"`
	ThreadID          string       `json:"threadId,omitempty"`
	Role              string       `json:"role"`
	Content           string       `json:"content"`
	ReplyToMessageID  string       `json:"replyToMessageId,omitempty"`
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

type OutboundDelivery struct {
	ID                string `json:"id"`
	Target            string `json:"target"`
	MessageID         string `json:"messageId"`
	EndpointID        string `json:"endpointId"`
	EndpointKind      string `json:"endpointKind"`
	ExternalMessageID string `json:"externalMessageId,omitempty"`
	Status            string `json:"status"`
	AttemptCount      uint32 `json:"attemptCount"`
	NextRetryTimeUnix int64  `json:"nextRetryTimeUnix,omitempty"`
	DeliveredTimeUnix int64  `json:"deliveredTimeUnix,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	RequestID         string `json:"requestId,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
	UpdatedUnix       int64  `json:"updatedUnix"`
}

type OutboundDeliveryListOptions struct {
	Target     string
	MessageID  string
	EndpointID string
	Statuses   []string
	Limit      int
}

type NotificationRoute struct {
	ID          string `json:"id"`
	Target      string `json:"target"`
	ThreadID    string `json:"threadId,omitempty"`
	EndpointID  string `json:"endpointId"`
	EventKind   string `json:"eventKind"`
	Preference  string `json:"preference"`
	Enabled     bool   `json:"enabled"`
	ConfigJSON  string `json:"configJson"`
	CreatedUnix int64  `json:"createdUnix"`
	UpdatedUnix int64  `json:"updatedUnix"`
}

type NotificationRouteListOptions struct {
	Target     string
	ThreadID   string
	EndpointID string
	EventKind  string
	Enabled    *bool
	Limit      int
}

type NotificationRouteResolveOptions struct {
	Target    string
	ThreadID  string
	EventKind string
	Limit     int
}

type NotificationRoutePatch struct {
	Target     *string
	ThreadID   *string
	EndpointID *string
	EventKind  *string
	Preference *string
	Enabled    *bool
	ConfigJSON *string
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

type MessageSearchOptions struct {
	Query        string
	Target       string
	SenderHandle string
	Sort         string
	Limit        int
}

type SavedMessage struct {
	ID             string  `json:"id"`
	Target         string  `json:"target"`
	MessageID      string  `json:"messageId"`
	SavedByUserID  string  `json:"savedByUserId,omitempty"`
	SavedByAgentID string  `json:"savedByAgentId,omitempty"`
	CreatedUnix    int64   `json:"createdUnix"`
	Message        Message `json:"message"`
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

type Reminder struct {
	ID                    string `json:"id"`
	Target                string `json:"target"`
	ScheduleKind          string `json:"scheduleKind"`
	Schedule              string `json:"schedule"`
	Prompt                string `json:"prompt,omitempty"`
	Enabled               bool   `json:"enabled"`
	NextRunUnix           int64  `json:"nextRunUnix"`
	LastRunUnix           int64  `json:"lastRunUnix,omitempty"`
	RunCount              int64  `json:"runCount"`
	LastError             string `json:"lastError,omitempty"`
	Title                 string `json:"title"`
	Status                string `json:"status"`
	MsgRef                string `json:"msgRef,omitempty"`
	RecurrenceRule        string `json:"recurrenceRule,omitempty"`
	RecurrenceDescription string `json:"recurrenceDescription,omitempty"`
	RecurrenceTimezone    string `json:"recurrenceTimezone,omitempty"`
	CancelToken           string `json:"cancelToken,omitempty"`
	CreatedUnix           int64  `json:"createdUnix"`
	UpdatedUnix           int64  `json:"updatedUnix"`
}

type ReminderPatch struct {
	Title                 *string
	ScheduleKind          *string
	Schedule              *string
	NextRunUnix           *int64
	RecurrenceRule        *string
	RecurrenceDescription *string
	RecurrenceTimezone    *string
}

type ReminderEvent struct {
	ID               string `json:"id"`
	ReminderID       string `json:"reminderId"`
	EventType        string `json:"eventType"`
	ActorType        string `json:"actorType"`
	ActorID          string `json:"actorId,omitempty"`
	OccurredTimeUnix int64  `json:"occurredTimeUnix"`
	NextFireTimeUnix int64  `json:"nextFireTimeUnix,omitempty"`
	Detail           string `json:"detail,omitempty"`
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
