package imadapter

// Scope defines where a capability is available: "all", "dm", "group", or comma-separated combinations.
type Scope string

const (
	ScopeAll   Scope = "all"
	ScopeDM    Scope = "dm"
	ScopeGroup Scope = "group"
)

// TextCapability describes plain text message support.
type TextCapability struct {
	Scope Scope `json:"scope"`
}

// FormattingCapability describes text formatting support.
type FormattingCapability struct {
	Scope Scope  `json:"scope"`
	Mode  string `json:"mode"` // "markdown", "html", "plain", etc.
}

// QuickRepliesCapability describes quick reply button support.
type QuickRepliesCapability struct {
	Scope    Scope `json:"scope"`
	MaxItems int   `json:"maxItems"`
}

// InlineButtonsCapability describes inline button/callback support.
type InlineButtonsCapability struct {
	Scope    Scope `json:"scope"`
	Callback bool  `json:"callback"`
	MaxItems int   `json:"maxItems,omitempty"`
}

// NativeCommandsCapability describes native command support.
type NativeCommandsCapability struct {
	Scope        Scope  `json:"scope"`
	ConfiguredBy string `json:"configuredBy"` // "provider", "nekode", etc.
}

// MessageEditCapability describes message edit support.
type MessageEditCapability struct {
	Scope Scope `json:"scope"`
}

// ReactionsCapability describes emoji reaction support.
type ReactionsCapability struct {
	Scope Scope `json:"scope"`
}

// ThreadsCapability describes thread/topic support.
type ThreadsCapability struct {
	Scope         Scope  `json:"scope"`
	ProviderModel string `json:"providerModel"` // "thread", "topic", "reply", etc.
}

// MediaUploadCapability describes media upload support.
type MediaUploadCapability struct {
	Scope    Scope `json:"scope"`
	MaxBytes int64 `json:"maxBytes"`
}

// MentionsCapability describes @mention support.
type MentionsCapability struct {
	Scope Scope `json:"scope"`
}

// InteractionCapabilities describes all interaction capabilities for a provider.
type InteractionCapabilities struct {
	Text           *TextCapability           `json:"text,omitempty"`
	Formatting     *FormattingCapability     `json:"formatting,omitempty"`
	QuickReplies   *QuickRepliesCapability   `json:"quickReplies,omitempty"`
	InlineButtons  *InlineButtonsCapability  `json:"inlineButtons,omitempty"`
	NativeCommands *NativeCommandsCapability `json:"nativeCommands,omitempty"`
	MessageEdit    *MessageEditCapability    `json:"messageEdit,omitempty"`
	Reactions      *ReactionsCapability      `json:"reactions,omitempty"`
	Threads        *ThreadsCapability        `json:"threads,omitempty"`
	MediaUpload    *MediaUploadCapability    `json:"mediaUpload,omitempty"`
	Mentions       *MentionsCapability       `json:"mentions,omitempty"`
}

// TelegramCapabilities returns the interaction capabilities for Telegram.
func TelegramCapabilities() *InteractionCapabilities {
	return &InteractionCapabilities{
		Text: &TextCapability{Scope: ScopeAll},
		Formatting: &FormattingCapability{
			Scope: "dm,group",
			Mode:  "markdown",
		},
		QuickReplies: &QuickRepliesCapability{
			Scope:    "dm,group",
			MaxItems: 8,
		},
		InlineButtons: &InlineButtonsCapability{
			Scope:    "dm,group",
			Callback: true,
		},
		NativeCommands: &NativeCommandsCapability{
			Scope:        ScopeAll,
			ConfiguredBy: "provider",
		},
		MessageEdit: &MessageEditCapability{
			Scope: "dm,group",
		},
		Reactions: &ReactionsCapability{
			Scope: "dm,group",
		},
		Threads: &ThreadsCapability{
			Scope:         ScopeGroup,
			ProviderModel: "topic",
		},
		MediaUpload: &MediaUploadCapability{
			Scope:    "dm,group",
			MaxBytes: 52428800, // 50MB
		},
		Mentions: &MentionsCapability{
			Scope: ScopeGroup,
		},
	}
}

// QQCapabilities returns the interaction capabilities for QQ.
func QQCapabilities() *InteractionCapabilities {
	return &InteractionCapabilities{
		Text: &TextCapability{Scope: ScopeAll},
		Formatting: &FormattingCapability{
			Scope: "dm,group",
			Mode:  "plain",
		},
		QuickReplies: &QuickRepliesCapability{
			Scope:    "dm,group",
			MaxItems: 5,
		},
		MediaUpload: &MediaUploadCapability{
			Scope:    "dm,group",
			MaxBytes: 10485760, // 10MB
		},
		Mentions: &MentionsCapability{
			Scope: ScopeGroup,
		},
	}
}

// FeishuCapabilities returns the interaction capabilities for Feishu.
func FeishuCapabilities() *InteractionCapabilities {
	return &InteractionCapabilities{
		Text: &TextCapability{Scope: ScopeAll},
		Formatting: &FormattingCapability{
			Scope: "dm,group",
			Mode:  "markdown",
		},
		MediaUpload: &MediaUploadCapability{
			Scope:    "dm,group",
			MaxBytes: 10485760, // 10MB
		},
		Mentions: &MentionsCapability{
			Scope: ScopeGroup,
		},
	}
}

// WeixinCapabilities returns the interaction capabilities for WeChat Official Account.
func WeixinCapabilities() *InteractionCapabilities {
	return &InteractionCapabilities{
		Text: &TextCapability{Scope: ScopeAll},
		Formatting: &FormattingCapability{
			Scope: ScopeAll,
			Mode:  "plain",
		},
		MediaUpload: &MediaUploadCapability{
			Scope:    ScopeAll,
			MaxBytes: 5242880, // 5MB
		},
	}
}

// ServerChanCapabilities returns the interaction capabilities for ServerChan.
func ServerChanCapabilities() *InteractionCapabilities {
	return &InteractionCapabilities{
		Text: &TextCapability{Scope: ScopeAll},
		Formatting: &FormattingCapability{
			Scope: ScopeAll,
			Mode:  "plain",
		},
	}
}

// TerminalCapabilities returns the interaction capabilities for Terminal.
func TerminalCapabilities() *InteractionCapabilities {
	return &InteractionCapabilities{
		Text: &TextCapability{Scope: ScopeAll},
		Formatting: &FormattingCapability{
			Scope: ScopeAll,
			Mode:  "plain",
		},
		QuickReplies: &QuickRepliesCapability{
			Scope:    ScopeAll,
			MaxItems: 10,
		},
	}
}
