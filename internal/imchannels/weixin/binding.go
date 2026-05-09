package weixin

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ca-x/nekode/internal/imbinding"
	"github.com/ca-x/nekode/internal/storage"
)

type BindingUpdate struct {
	Session    imbinding.Session
	ConfigJSON string
	Bound      bool
}

func SupportsILinkBinding(endpoint storage.InteractionEndpoint) bool {
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		return false
	}
	return cfg.normalize().Mode == "ilink"
}

func StartILinkBindingSession(endpoint storage.InteractionEndpoint, bindings *imbinding.Store, session imbinding.Session) (imbinding.Session, error) {
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		return session, err
	}
	cfg = cfg.normalize()
	if cfg.Mode != "ilink" {
		return session, imbinding.ErrEndpointUnsupported
	}
	client := NewILinkClient(cfg.BaseURL, cfg.CDNBaseURL, "")
	qr, err := client.GetQRCode("")
	if err != nil {
		return bindings.Update(endpoint, session.ID, imbinding.Patch{Status: imbinding.StatusFailed, Detail: err.Error()})
	}
	return bindings.Update(endpoint, session.ID, imbinding.Patch{
		Status:      imbinding.StatusPending,
		QRPayload:   strings.TrimSpace(qr.QRCode),
		QRImageURL:  qrImageURL(qr.QRCodeImgContent),
		ExpiresUnix: time.Now().Add(5 * time.Minute).Unix(),
		Detail:      "Waiting for Weixin QR scan.",
	})
}

func PollILinkBindingSession(endpoint storage.InteractionEndpoint, bindings *imbinding.Store, session imbinding.Session) (BindingUpdate, error) {
	cfg, err := ConfigFromEndpoint(endpoint)
	if err != nil {
		return BindingUpdate{Session: session}, err
	}
	cfg = cfg.normalize()
	if cfg.Mode != "ilink" || session.QRPayload == "" || terminalBindingStatus(session.Status) {
		return BindingUpdate{Session: session}, nil
	}
	client := NewILinkClient(cfg.BaseURL, cfg.CDNBaseURL, cfg.BotToken)
	status, err := client.GetQRCodeStatus(session.QRPayload, "")
	if err != nil {
		updated, updateErr := bindings.Update(endpoint, session.ID, imbinding.Patch{Status: imbinding.StatusFailed, Detail: err.Error()})
		if updateErr != nil {
			return BindingUpdate{Session: session}, updateErr
		}
		return BindingUpdate{Session: updated}, err
	}
	patch := imbinding.Patch{Status: mapILinkQRStatus(status), Detail: "Waiting for Weixin QR scan."}
	if patch.Status == imbinding.StatusScanned {
		patch.Detail = "Weixin QR scanned; waiting for authorization."
	}
	boundConfig := ""
	bound := false
	if strings.TrimSpace(status.BotToken) != "" {
		patch.Status = imbinding.StatusBound
		patch.Detail = "Weixin QR binding completed."
		bound = true
		boundConfig, err = mergeILinkBindingConfig(endpoint.ConfigJSON, status)
		if err != nil {
			return BindingUpdate{Session: session}, err
		}
	}
	updated, err := bindings.Update(endpoint, session.ID, patch)
	if err != nil {
		return BindingUpdate{Session: session}, err
	}
	return BindingUpdate{Session: updated, ConfigJSON: boundConfig, Bound: bound}, nil
}

func mergeILinkBindingConfig(rawConfig string, status *ILinkQRCodeStatusResponse) (string, error) {
	config := map[string]any{}
	if strings.TrimSpace(rawConfig) != "" {
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return "", err
		}
	}
	config["mode"] = "ilink"
	config["bot_token"] = strings.TrimSpace(status.BotToken)
	if strings.TrimSpace(status.ILinkBotID) != "" {
		config["bot_id"] = strings.TrimSpace(status.ILinkBotID)
	}
	if strings.TrimSpace(status.ILinkUserID) != "" {
		config["user_id"] = strings.TrimSpace(status.ILinkUserID)
	}
	if strings.TrimSpace(status.BaseURL) != "" {
		config["base_url"] = strings.TrimSpace(status.BaseURL)
	}
	data, err := json.Marshal(config)
	return string(data), err
}

func mapILinkQRStatus(status *ILinkQRCodeStatusResponse) string {
	if status == nil {
		return imbinding.StatusPending
	}
	value := strings.ToLower(strings.TrimSpace(status.Status))
	switch {
	case strings.TrimSpace(status.BotToken) != "":
		return imbinding.StatusBound
	case strings.Contains(value, "scan"):
		return imbinding.StatusScanned
	case strings.Contains(value, "expire"):
		return imbinding.StatusExpired
	case strings.Contains(value, "fail"), strings.Contains(value, "cancel"), strings.Contains(value, "reject"):
		return imbinding.StatusFailed
	default:
		return imbinding.StatusPending
	}
}

func qrImageURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "data:") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "data:image/png;base64," + value
}

func terminalBindingStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case imbinding.StatusBound, imbinding.StatusExpired, imbinding.StatusFailed:
		return true
	default:
		return false
	}
}
