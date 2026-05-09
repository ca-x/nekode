package weixin

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	ILinkDefaultBaseURL        = "https://ilinkai.weixin.qq.com"
	ILinkDefaultCDNBaseURL     = "https://novac2c.cdn.weixin.qq.com"
	ILinkDefaultChannelVersion = "1.0.0"
	ILinkMessageTypeBot        = 2
	ILinkMessageStateFinish    = 2
	ILinkItemTypeText          = 1

	ilinkAuthorizationType = "ilink_bot_token"
	ilinkDefaultTimeout    = 40 * time.Second
)

var ErrILinkSessionExpired = errors.New("weixin ilink: session expired")

// ILinkClient is fork-adapted from Stella plugins/channels/weixin/client.go.
// It keeps the request surface close to Stella while routing credentials through
// Nekode InteractionEndpoint config and provider availability gating.
type ILinkClient struct {
	baseURL    string
	cdnBaseURL string
	token      string
	httpClient *resty.Client
}

type ILinkConfig struct {
	BotToken string
	BaseURL  string
	BotID    string
	UserID   string
}

type ILinkMessage struct {
	Seq          int64              `json:"seq,omitempty"`
	MessageID    int64              `json:"message_id,omitempty"`
	FromUserID   string             `json:"from_user_id,omitempty"`
	ToUserID     string             `json:"to_user_id,omitempty"`
	ClientID     string             `json:"client_id,omitempty"`
	CreateTimeMS int64              `json:"create_time_ms,omitempty"`
	UpdateTimeMS int64              `json:"update_time_ms,omitempty"`
	DeleteTimeMS int64              `json:"delete_time_ms,omitempty"`
	SessionID    string             `json:"session_id,omitempty"`
	GroupID      string             `json:"group_id,omitempty"`
	MessageType  int                `json:"message_type,omitempty"`
	MessageState int                `json:"message_state,omitempty"`
	ItemList     []ILinkMessageItem `json:"item_list,omitempty"`
	ContextToken string             `json:"context_token,omitempty"`
}

type ILinkMessageItem struct {
	Type      int             `json:"type,omitempty"`
	TextItem  *ILinkTextItem  `json:"text_item,omitempty"`
	ImageItem *ILinkImageItem `json:"image_item,omitempty"`
	VoiceItem *ILinkVoiceItem `json:"voice_item,omitempty"`
	FileItem  *ILinkFileItem  `json:"file_item,omitempty"`
	VideoItem *ILinkVideoItem `json:"video_item,omitempty"`
}

type ILinkTextItem struct {
	Text string `json:"text,omitempty"`
}

type ILinkImageItem struct {
	URL    string `json:"url,omitempty"`
	AESKey string `json:"aeskey,omitempty"`
}

type ILinkVoiceItem struct {
	Text string `json:"text,omitempty"`
}

type ILinkFileItem struct {
	FileName string `json:"file_name,omitempty"`
	Len      string `json:"len,omitempty"`
}

type ILinkVideoItem struct {
	VideoSize int64 `json:"video_size,omitempty"`
}

type ILinkBaseInfo struct {
	ChannelVersion string `json:"channel_version"`
}

type ILinkGetUpdatesRequest struct {
	GetUpdatesBuf string        `json:"get_updates_buf"`
	BaseInfo      ILinkBaseInfo `json:"base_info"`
}

type ILinkGetUpdatesResponse struct {
	Ret                  int            `json:"ret"`
	ErrCode              int            `json:"errcode,omitempty"`
	ErrMsg               string         `json:"errmsg,omitempty"`
	Msgs                 []ILinkMessage `json:"msgs,omitempty"`
	GetUpdatesBuf        string         `json:"get_updates_buf,omitempty"`
	LongPollingTimeoutMS int            `json:"longpolling_timeout_ms,omitempty"`
}

type ILinkSendMessageRequest struct {
	Msg      ILinkMessage  `json:"msg"`
	BaseInfo ILinkBaseInfo `json:"base_info"`
}

type ILinkSendMessageResponse struct {
	Ret     int    `json:"ret,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
}

type ILinkGetConfigRequest struct {
	ILinkUserID  string        `json:"ilink_user_id"`
	ContextToken string        `json:"context_token,omitempty"`
	BaseInfo     ILinkBaseInfo `json:"base_info"`
}

type ILinkGetConfigResponse struct {
	Ret          int    `json:"ret"`
	ErrCode      int    `json:"errcode,omitempty"`
	ErrMsg       string `json:"errmsg,omitempty"`
	TypingTicket string `json:"typing_ticket,omitempty"`
}

type ILinkQRCodeResponse struct {
	QRCode           string `json:"qrcode,omitempty"`
	QRCodeImgContent string `json:"qrcode_img_content,omitempty"`
}

type ILinkQRCodeStatusResponse struct {
	Status      string `json:"status,omitempty"`
	BotToken    string `json:"bot_token,omitempty"`
	ILinkBotID  string `json:"ilink_bot_id,omitempty"`
	ILinkUserID string `json:"ilink_user_id,omitempty"`
	BaseURL     string `json:"baseurl,omitempty"`
}

func NewILinkClient(baseURL, cdnBaseURL, token string) *ILinkClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = ILinkDefaultBaseURL
	}
	if strings.TrimSpace(cdnBaseURL) == "" {
		cdnBaseURL = ILinkDefaultCDNBaseURL
	}
	return &ILinkClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
		token:      strings.TrimSpace(token),
		httpClient: resty.New().SetTimeout(ilinkDefaultTimeout),
	}
}

func (c *ILinkClient) GetQRCode(skRouteTag string) (*ILinkQRCodeResponse, error) {
	r := c.httpClient.R()
	if skRouteTag != "" {
		r.SetHeader("SKRouteTag", skRouteTag)
	}
	var result ILinkQRCodeResponse
	resp, err := r.SetResult(&result).Get(c.baseURL + "/ilink/bot/get_bot_qrcode?bot_type=3")
	if err != nil {
		return nil, fmt.Errorf("weixin ilink get qrcode: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("weixin ilink get qrcode HTTP %d", resp.StatusCode())
	}
	if result.QRCode == "" && result.QRCodeImgContent == "" && len(resp.Body()) > 0 {
		_ = json.Unmarshal(resp.Body(), &result)
	}
	return &result, nil
}

func (c *ILinkClient) GetQRCodeStatus(qrcode, skRouteTag string) (*ILinkQRCodeStatusResponse, error) {
	r := c.httpClient.R().
		SetHeader("iLink-App-ClientVersion", "1").
		SetQueryParam("qrcode", qrcode)
	if skRouteTag != "" {
		r.SetHeader("SKRouteTag", skRouteTag)
	}
	var result ILinkQRCodeStatusResponse
	resp, err := r.SetResult(&result).Get(c.baseURL + "/ilink/bot/get_qrcode_status")
	if err != nil {
		return nil, fmt.Errorf("weixin ilink qrcode status: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("weixin ilink qrcode status HTTP %d", resp.StatusCode())
	}
	if result.Status == "" && result.BotToken == "" && len(resp.Body()) > 0 {
		_ = json.Unmarshal(resp.Body(), &result)
	}
	return &result, nil
}

func (c *ILinkClient) GetUpdates(buf, channelVersion string, timeout time.Duration) (*ILinkGetUpdatesResponse, error) {
	if channelVersion == "" {
		channelVersion = ILinkDefaultChannelVersion
	}
	client := c.httpClient
	if timeout > 0 {
		client = resty.New().SetTimeout(timeout)
	}
	var result ILinkGetUpdatesResponse
	resp, err := client.R().
		SetHeaders(c.commonHeaders()).
		SetBody(ILinkGetUpdatesRequest{GetUpdatesBuf: buf, BaseInfo: ILinkBaseInfo{ChannelVersion: channelVersion}}).
		SetResult(&result).
		Post(c.baseURL + "/ilink/bot/getupdates")
	if err != nil {
		return nil, fmt.Errorf("weixin ilink getupdates: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("weixin ilink getupdates HTTP %d", resp.StatusCode())
	}
	return &result, ilinkCheckError(result.Ret, result.ErrCode, result.ErrMsg)
}

func (c *ILinkClient) SendMessage(msg ILinkMessage, channelVersion string) error {
	if channelVersion == "" {
		channelVersion = ILinkDefaultChannelVersion
	}
	var result ILinkSendMessageResponse
	_, err := c.httpClient.R().
		SetHeaders(c.commonHeaders()).
		SetBody(ILinkSendMessageRequest{Msg: msg, BaseInfo: ILinkBaseInfo{ChannelVersion: channelVersion}}).
		SetResult(&result).
		Post(c.baseURL + "/ilink/bot/sendmessage")
	if err != nil {
		return fmt.Errorf("weixin ilink sendmessage: %w", err)
	}
	return ilinkCheckError(result.Ret, result.ErrCode, result.ErrMsg)
}

func (c *ILinkClient) GetConfig(userID, contextToken, channelVersion string) (*ILinkGetConfigResponse, error) {
	if channelVersion == "" {
		channelVersion = ILinkDefaultChannelVersion
	}
	var result ILinkGetConfigResponse
	resp, err := c.httpClient.R().
		SetHeaders(c.commonHeaders()).
		SetBody(ILinkGetConfigRequest{ILinkUserID: userID, ContextToken: contextToken, BaseInfo: ILinkBaseInfo{ChannelVersion: channelVersion}}).
		SetResult(&result).
		Post(c.baseURL + "/ilink/bot/getconfig")
	if err != nil {
		return nil, fmt.Errorf("weixin ilink getconfig: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("weixin ilink getconfig HTTP %d", resp.StatusCode())
	}
	return &result, ilinkCheckError(result.Ret, result.ErrCode, result.ErrMsg)
}

func RandomILinkClientID(prefix string) string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	seed := binary.BigEndian.Uint64(buf[:])
	if strings.TrimSpace(prefix) == "" {
		prefix = "nekode-weixin"
	}
	return prefix + ":" + strconv.FormatUint(seed, 36)
}

func (c *ILinkClient) commonHeaders() map[string]string {
	return map[string]string{
		"Content-Type":      "application/json",
		"AuthorizationType": ilinkAuthorizationType,
		"Authorization":     "Bearer " + c.token,
		"X-WECHAT-UIN":      randomWechatUIN(),
	}
}

func randomWechatUIN() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(binary.BigEndian.Uint32(buf[:])), 10)))
}

func ilinkCheckError(ret, errcode int, errmsg string) error {
	if ret == -14 || errcode == -14 {
		return ErrILinkSessionExpired
	}
	if ret != 0 {
		return fmt.Errorf("weixin ilink API error ret=%d errcode=%d: %s", ret, errcode, errmsg)
	}
	if errcode != 0 {
		return fmt.Errorf("weixin ilink API error errcode=%d: %s", errcode, errmsg)
	}
	return nil
}
