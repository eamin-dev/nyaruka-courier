package tembachat

import (
	"bytes"
	"context"
	"net/http"

	"github.com/nyaruka/courier"
	"github.com/nyaruka/courier/handlers"
	"github.com/nyaruka/gocommon/jsonx"
	"github.com/nyaruka/gocommon/urns"
)

var (
	defaultSendURL = "http://chatserver:8070/send"
)

func init() {
	courier.RegisterHandler(newHandler())
}

type handler struct {
	handlers.BaseHandler
}

func newHandler() courier.ChannelHandler {
	return &handler{handlers.NewBaseHandler(courier.ChannelType("TWC"), "Temba Chat")}
}

// Initialize is called by the engine once everything is loaded
func (h *handler) Initialize(s courier.Server) error {
	h.SetServer(s)
	s.AddHandlerRoute(h, http.MethodPost, "receive", courier.ChannelLogTypeMsgReceive, handlers.JSONPayload(h, h.receiveMessage))
	return nil
}

type receivePayload struct {
	Type string `json:"type" validate:"required"`
	Msg  struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	} `json:"msg"`
	Chat struct {
		ChatID string `json:"chat_id"`
	} `json:"chat"`
}

// receiveMessage is our HTTP handler function for incoming messages
func (h *handler) receiveMessage(ctx context.Context, c courier.Channel, w http.ResponseWriter, r *http.Request, payload *receivePayload, clog *courier.ChannelLog) ([]courier.Event, error) {
	if payload.Type == "msg_in" {
		urn, err := urns.NewURNFromParts(urns.WebChatScheme, payload.Msg.ChatID, "", "")
		if err != nil {
			return nil, handlers.WriteAndLogRequestError(ctx, h, c, w, r, err)
		}

		msg := h.Backend().NewIncomingMsg(c, urn, payload.Msg.Text, "", clog)
		return handlers.WriteMsgsAndResponse(ctx, h, []courier.MsgIn{msg}, w, r, clog)
	} else if payload.Type == "chat_started" {
		urn, err := urns.NewURNFromParts(urns.WebChatScheme, payload.Chat.ChatID, "", "")
		if err != nil {
			return nil, handlers.WriteAndLogRequestError(ctx, h, c, w, r, err)
		}

		evt := h.Backend().NewChannelEvent(c, courier.EventTypeNewConversation, urn, clog)

		if err := h.Backend().WriteChannelEvent(ctx, evt, clog); err != nil {
			return nil, err
		}
		return []courier.Event{evt}, courier.WriteChannelEventSuccess(w, evt)
	}
	return nil, handlers.WriteAndLogRequestIgnored(ctx, h, c, w, r, "")
}

type sendPayload struct {
	MsgID  courier.MsgID     `json:"msg_id"`
	ChatID string            `json:"chat_id"`
	Text   string            `json:"text"`
	Origin courier.MsgOrigin `json:"origin"`
	UserID courier.UserID    `json:"user_id,omitempty"`
}

func (h *handler) Send(ctx context.Context, msg courier.MsgOut, clog *courier.ChannelLog) (courier.StatusUpdate, error) {
	sendURL := msg.Channel().StringConfigForKey(courier.ConfigSendURL, defaultSendURL)
	sendURL += "?channel=" + string(msg.Channel().UUID())

	payload := &sendPayload{
		MsgID:  msg.ID(),
		ChatID: msg.URN().Path(),
		Text:   msg.Text(),
		Origin: msg.Origin(),
		UserID: msg.UserID(),
	}
	req, _ := http.NewRequest("POST", sendURL, bytes.NewReader(jsonx.MustMarshal(payload)))

	status := h.Backend().NewStatusUpdate(msg.Channel(), msg.ID(), courier.MsgStatusWired, clog)

	resp, _, err := h.RequestHTTP(req, clog)
	if err != nil || resp.StatusCode/100 != 2 {
		status.SetStatus(courier.MsgStatusErrored)
	}

	return status, nil
}
