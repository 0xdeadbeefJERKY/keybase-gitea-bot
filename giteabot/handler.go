package giteabot

import (
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/keybase/go-keybase-chat-bot/kbchat/types/chat1"
	"github.com/keybase/managed-bots/base"
)

type Handler struct {
	*base.DebugOutput

	stats      *base.StatsRegistry
	kbc        *kbchat.API
	db         *DB
	httpPrefix string
	secret     string
	giteaURL   string
}

var _ base.Handler = (*Handler)(nil)

func NewHandler(stats *base.StatsRegistry, kbc *kbchat.API, debugConfig *base.ChatDebugOutputConfig,
	db *DB, httpPrefix string, secret string, giteaURL string) *Handler {
	return &Handler{
		DebugOutput: base.NewDebugOutput("Handler", debugConfig),
		stats:       stats.SetPrefix("Handler"),
		kbc:         kbc,
		db:          db,
		httpPrefix:  httpPrefix,
		secret:      secret,
		giteaURL:	 giteaURL,
	}
}

func (h *Handler) HandleNewConv(conv chat1.ConvSummary) error {
	welcomeMsg := "Hi! I can notify you whenever something happens on a Gitea repository. Seems like I'm alive because you're getting this message. Happy days."
	welcomeMsg += "\n\nTo get started, try `!gitea subscribe <owner/repo>`."
	return base.HandleNewTeam(h.stats, h.DebugOutput, h.kbc, conv, welcomeMsg)
}

func (h *Handler) HandleCommand(msg chat1.MsgSummary) error {
	if msg.Content.Text == nil {
		return nil
	}

	cmd := strings.ToLower(strings.TrimSpace(msg.Content.Text.Body))
	if !strings.HasPrefix(cmd, "!gitea") {
		return nil
	}

	switch {
	case strings.HasPrefix(cmd, "!gitea list"):
		h.stats.Count("list")
		return h.handleListSubscriptions(msg)
	case strings.HasPrefix(cmd, "!gitea subscribe"):
		h.stats.Count("subscribe")
		return h.handleSubscribe(cmd, msg, true)
	case strings.HasPrefix(cmd, "!gitea unsubscribe"):
		h.stats.Count("unsubscribe")
		return h.handleSubscribe(cmd, msg, false)
	default:
		h.ChatEcho(msg.ConvID, "Unknown command.", cmd)
	}
	return nil
}

func (h *Handler) handleListSubscriptions(msg chat1.MsgSummary) (err error) {
	subscriptions, err := h.db.GetAllSubscriptionsForConvID(msg.ConvID)
	if err != nil {
		return fmt.Errorf("error getting current repos: %s", err)
	}

	if len(subscriptions) == 0 {
		h.ChatEcho(msg.ConvID, "Not subscribed to any projects yet.")
		return nil
	}

	var res string
	for _, repo := range subscriptions {
		res += fmt.Sprintf("- *%s*\n", repo)
	}
	h.ChatEcho(msg.ConvID, res)
	return nil
}

func (h *Handler) handleSubscribe(cmd string, msg chat1.MsgSummary, create bool) (err error) {
	toks, userErr, err := base.SplitTokens(cmd)
	if err != nil {
		return err
	} else if userErr != "" {
		h.ChatEcho(msg.ConvID, userErr)
		return nil
	}

	args := toks[2:]
	if len(args) < 1 {
		h.ChatEcho(msg.ConvID, "bad args for subscribe: %v", args)
		return nil
	}

	repo := args[0]
	alreadyExists, err := h.db.GetSubscriptionForRepoExists(msg.ConvID, repo)
	if err != nil {
		return fmt.Errorf("error checking subscription: %s", err)
	}

	parsedRepo := strings.Split(repo, "/")
	if len(parsedRepo) <= 1 {
		h.ChatEcho(msg.ConvID, "invalid repo: %q, expected `<owner/repo>`", repo)
		return nil
	}
	if create {
		if !alreadyExists {
			err = h.db.CreateSubscription(msg.ConvID, repo, base.IdentifierFromMsg(msg))
			if err != nil {
				return fmt.Errorf("error creating subscription: %s", err)
			}
			_, err = h.kbc.SendMessageByTlfName(msg.Sender.Username, formatSetupInstructions(h.giteaURL, repo, msg, h.httpPrefix, h.secret))
			if err != nil {
				return fmt.Errorf("error sending message: %s", err)
			}
			if !base.IsDirectPrivateMessage(h.kbc.GetUsername(), msg.Sender.Username, msg.Channel) {
				h.ChatEcho(msg.ConvID, "OK! I've sent a message to @%s to authorize me.", msg.Sender.Username)
			}
			return nil
		}

		h.ChatEcho(msg.ConvID, "You're already receiving notifications for `%s` here!", repo)
		return nil
	}

	if alreadyExists {
		err = h.db.DeleteSubscriptionsForRepo(msg.ConvID, repo)
		if err != nil {
			return fmt.Errorf("error deleting subscriptions: %s", err)
		}
		h.ChatEcho(msg.ConvID, "Okay, you won't receive updates for `%s` here.", repo)
		return nil
	}

	h.ChatEcho(msg.ConvID, "You aren't subscribed to updates for `%s`!", repo)
	return nil
}