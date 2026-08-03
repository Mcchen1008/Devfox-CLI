package history

// Msg 单条对话消息
type Msg struct {
	Role         string
	Content      string
	ToolCallID   string
	RawAssistant string // assistant 原始消息（含 tool_calls），用于回传
	IsUser       bool
}

// History 对话历史（保留最近 N 轮用户消息）
type History struct {
	items        []Msg
	MaxUserTurns int
}

func New(maxUserTurns int) *History {
	return &History{MaxUserTurns: maxUserTurns}
}

func (h *History) Add(role, content, toolCallID, rawAssistant string) {
	h.items = append(h.items, Msg{
		Role:         role,
		Content:      content,
		ToolCallID:   toolCallID,
		RawAssistant: rawAssistant,
		IsUser:       role == "user",
	})
}

func (h *History) Get() []Msg {
	return h.items
}

func (h *History) UserCount() int {
	n := 0
	for i := range h.items {
		if h.items[i].IsUser {
			n++
		}
	}
	return n
}

func (h *History) Clear() {
	h.items = nil
}

// Trim 裁剪超过上限的旧消息（连同最后一个被移除 user 之后的消息）
func (h *History) Trim() {
	uc := h.UserCount()
	if uc <= h.MaxUserTurns || h.MaxUserTurns == 0 {
		return
	}
	excess := uc - h.MaxUserTurns
	removed := 0
	drop := 0
	for drop < len(h.items) && removed < excess {
		if h.items[drop].IsUser {
			removed++
		}
		drop++
	}
	for drop < len(h.items) && !h.items[drop].IsUser {
		drop++
	}
	h.items = h.items[drop:]
}
