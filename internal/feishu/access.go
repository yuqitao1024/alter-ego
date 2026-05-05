package feishu

import "github.com/yuqitao1024/alter-ego/internal/channel"

func Allowed(cfg Config, event channel.MessageEvent) bool {
	if event.Sender.ID == "" || !cfg.AllowUsers[event.Sender.ID] {
		return false
	}

	switch event.Conversation.Kind {
	case channel.ConversationDirect:
		return true
	case channel.ConversationGroup:
		if event.Conversation.ID == "" || !cfg.AllowGroups[event.Conversation.ID] {
			return false
		}
		if cfg.RequireMention && !event.MentionedBot {
			return false
		}
		return true
	default:
		return false
	}
}
