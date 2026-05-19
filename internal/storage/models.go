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

const (
	IMChatAuthRequestStatusPending  = "pending"
	IMChatAuthRequestStatusApproved = "approved"
	IMChatAuthRequestStatusRejected = "rejected"
	IMChatAuthRequestStatusExpired  = "expired"
)

type IMChatAuthRequest struct {
	ID                string `json:"id"`
	EndpointID        string `json:"endpointId"`
	Provider          string `json:"provider"`
	ConversationID    string `json:"conversationId"`
	ExternalThreadID  string `json:"externalThreadId,omitempty"`
	ChatTitle         string `json:"chatTitle,omitempty"`
	SenderExternalID  string `json:"senderExternalId,omitempty"`
	TokenHash         string `json:"-"`
	TokenPrefix       string `json:"tokenPrefix,omitempty"`
	Status            string `json:"status"`
	RequestedTarget   string `json:"requestedTarget,omitempty"`
	RequestedThreadID string `json:"requestedThreadId,omitempty"`
	ExpiresUnix       int64  `json:"expiresUnix,omitempty"`
	ResolvedByUserID  string `json:"resolvedByUserId,omitempty"`
	ResolvedUnix      int64  `json:"resolvedUnix,omitempty"`
	CreatedUnix       int64  `json:"createdUnix"`
	UpdatedUnix       int64  `json:"updatedUnix"`
}

type IMChatAuthRequestListOptions struct {
	EndpointID     string
	Status         string
	IncludeExpired bool
	Limit          int
}

type IMChatSubscription struct {
	ID                    string `json:"id"`
	EndpointID            string `json:"endpointId"`
	Provider              string `json:"provider"`
	ConversationID        string `json:"conversationId"`
	ExternalThreadID      string `json:"externalThreadId,omitempty"`
	ChatTitle             string `json:"chatTitle,omitempty"`
	Target                string `json:"target,omitempty"`
	ThreadID              string `json:"threadId,omitempty"`
	SenderExternalID      string `json:"senderExternalId,omitempty"`
	AuthorizedByRequestID string `json:"authorizedByRequestId,omitempty"`
	Subscribed            bool   `json:"subscribed"`
	Verbose               bool   `json:"verbose"`
	AuthorizedUnix        int64  `json:"authorizedUnix,omitempty"`
	SubscribedUnix        int64  `json:"subscribedUnix,omitempty"`
	CreatedUnix           int64  `json:"createdUnix"`
	UpdatedUnix           int64  `json:"updatedUnix"`
}

type IMChatSubscriptionPatch struct {
	ChatTitle  *string
	Target     *string
	ThreadID   *string
	Subscribed *bool
	Verbose    *bool
}

type IMChatSubscriptionListOptions struct {
	EndpointID string
	Provider   string
	Subscribed *bool
	Limit      int
}

type ChannelSummary struct {
	Target          string `json:"target"`
	DisplayName     string `json:"displayName"`
	ChannelType     string `json:"channelType"`
	Visibility      string `json:"visibility"`
	Joined          bool   `json:"joined"`
	MemberCount     int    `json:"memberCount"`
	CurrentUserRole string `json:"currentUserRole,omitempty"`
	CreatedByUserID string `json:"createdByUserId,omitempty"`
	CreatedUnix     int64  `json:"createdUnix,omitempty"`
	UpdatedUnix     int64  `json:"updatedUnix,omitempty"`
}

type ChannelPatch struct {
	DisplayName *string
	Visibility  *string
}

type ChannelListOptions struct {
	JoinedOnly bool
	UserID     string
	Limit      int
}

type ChannelMember struct {
	Target         string `json:"target"`
	MemberID       string `json:"memberId"`
	Username       string `json:"username,omitempty"`
	DisplayName    string `json:"displayName"`
	Kind           string `json:"kind"`
	Role           string `json:"role"`
	JoinedTimeUnix int64  `json:"joinedTimeUnix"`
	UpdatedUnix    int64  `json:"updatedUnix,omitempty"`
}

type ChannelMemberPatch struct {
	Username    *string
	DisplayName *string
	Role        *string
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
	// Kind classifies the message for filtering and promotion. Valid values:
	// note (default), decision, blocker, status.
	Kind string `json:"kind,omitempty"`
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
	ReadyUnix  int64
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
	Query         string
	Target        string
	SenderHandle  string
	HasAttachment bool
	Sort          string
	Limit         int
}

type SavedMessageListOptions struct {
	Target        string
	UserID        string
	AgentID       string
	Query         string
	HasAttachment bool
	Limit         int
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

type TaskAttempt struct {
	ID                 string `json:"id"`
	TaskID             string `json:"taskId"`
	Attempt            uint32 `json:"attempt"`
	RunID              string `json:"runId,omitempty"`
	AgentID            string `json:"agentId,omitempty"`
	ClaimLeaseID       string `json:"claimLeaseId,omitempty"`
	Status             string `json:"status"`
	OutputJSON         string `json:"outputJson,omitempty"`
	OutputDigest       string `json:"outputDigest,omitempty"`
	OutputSignature    string `json:"outputSignature,omitempty"`
	SignaturePublicKey string `json:"signaturePublicKey,omitempty"`
	ErrorMessage       string `json:"errorMessage,omitempty"`
	ClaimedUnix        int64  `json:"claimedUnix,omitempty"`
	StartedUnix        int64  `json:"startedUnix,omitempty"`
	CompletedUnix      int64  `json:"completedUnix,omitempty"`
	UpdatedUnix        int64  `json:"updatedUnix"`
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

// ChannelDecision is a governance record attached to a channel. Status is
// one of: proposed, ratified, rejected, retired.
type ChannelDecision struct {
	ID                   string `json:"id"`
	Target               string `json:"target"`
	Title                string `json:"title"`
	Body                 string `json:"body"`
	Status               string `json:"status"`
	ProposerID           string `json:"proposerId"`
	ProposerKind         string `json:"proposerKind"`
	CreatedUnix          int64  `json:"createdUnix"`
	RatifiedUnix         int64  `json:"ratifiedUnix,omitempty"`
	RetiredUnix          int64  `json:"retiredUnix,omitempty"`
	RetiredBy            string `json:"retiredBy,omitempty"`
	RetireReason         string `json:"retireReason,omitempty"`
	SupersedesDecisionID string `json:"supersedesDecisionId,omitempty"`
	ApproveCount         uint32 `json:"approveCount"`
	RejectCount          uint32 `json:"rejectCount"`
	AbstainCount         uint32 `json:"abstainCount"`
}

// ChannelDecisionListOptions filters the list of decisions.
type ChannelDecisionListOptions struct {
	Target       string
	StatusFilter []string // empty means all
	Limit        int
	AfterCreated int64
}

// ChannelDecisionVote is one voter's stance. Decision is one of:
// approve, reject, abstain.
type ChannelDecisionVote struct {
	ID         string `json:"id"`
	DecisionID string `json:"decisionId"`
	VoterID    string `json:"voterId"`
	VoterKind  string `json:"voterKind"`
	Decision   string `json:"decision"`
	VotedUnix  int64  `json:"votedUnix"`
	Reason     string `json:"reason,omitempty"`
}

// AgentRun is one agent session. EventCount is maintained by the server as
// it ingests events.
type AgentRun struct {
	ID          string `json:"id"`
	AgentID     string `json:"agentId"`
	ComputerID  string `json:"computerId"`
	StartedUnix int64  `json:"startedUnix"`
	EndedUnix   int64  `json:"endedUnix,omitempty"`
	ExitCode    int32  `json:"exitCode"`
	Summary     string `json:"summary,omitempty"`
	Error       string `json:"error,omitempty"`
	EventCount  uint32 `json:"eventCount"`
}

// AgentRunListOptions filters agent run listings.
type AgentRunListOptions struct {
	AgentID       string
	ComputerID    string
	Limit         int
	BeforeStarted int64
}

// AgentRunEvent is one lifecycle event inside a run. Phase is one of:
// start, tool_call, tool_result, error, output, end.
type AgentRunEvent struct {
	ID           string `json:"id"`
	RunID        string `json:"runId"`
	AtUnixNano   int64  `json:"atUnixNano"`
	Phase        string `json:"phase"`
	Summary      string `json:"summary,omitempty"`
	PayloadJSON  string `json:"payloadJson,omitempty"`
	ExitCode     int32  `json:"exitCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// AgentRunSearchOptions queries agent_run_events_fts.
type AgentRunSearchOptions struct {
	Query      string
	AgentID    string
	ComputerID string
	Limit      int
}

// AgentRunSearchHit pairs a matching event with its run + a highlight excerpt.
type AgentRunSearchHit struct {
	Run       AgentRun      `json:"run"`
	Event     AgentRunEvent `json:"event"`
	Highlight string        `json:"highlight,omitempty"`
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
