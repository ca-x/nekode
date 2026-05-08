package imadapter

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const typingCursor = " ▍"

type StreamState struct {
	Text        string
	ActiveTool  string
	StartedUnix int64
	UpdatedUnix int64
	Done        bool
}

func RenderStream(state StreamState, maxLen int) string {
	text := state.Text
	if strings.TrimSpace(text) == "" && state.ActiveTool == "" {
		text = "Thinking..."
	}
	if state.ActiveTool != "" && !state.Done {
		text = strings.TrimRight(text, "\n") + "\n\n" + state.ActiveTool
	}
	if state.Done && state.StartedUnix > 0 && state.UpdatedUnix >= state.StartedUnix {
		elapsed := time.Duration(state.UpdatedUnix-state.StartedUnix) * time.Second
		text = strings.TrimRight(text, "\n") + fmt.Sprintf("\n\nResponse time: %.1fs", elapsed.Seconds())
	}
	if !state.Done {
		text += typingCursor
	}
	if maxLen <= 0 {
		return text
	}
	return truncateUTF8(text, maxLen)
}

func truncateUTF8(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	cutAt := maxLen - 3
	for cutAt > 0 && !utf8.RuneStart(value[cutAt]) {
		cutAt--
	}
	return value[:cutAt] + "..."
}
