package clicksend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/buger/jsonparser"
	"github.com/nyaruka/courier"
	"github.com/nyaruka/courier/handlers"
	"github.com/pkg/errors"
)

var (
	maxMsgLength = 1224
	sendURL      = "https://rest.clicksend.com/v3/sms/send"
)

func init() {
	courier.RegisterHandler(newHandler())
}

type handler struct {
	handlers.BaseHandler
}

func newHandler() courier.ChannelHandler {
	return &handler{handlers.NewBaseHandler(courier.ChannelType("CS"), "ClickSend")}
}

// Initialize is called by the engine once everything is loaded
func (h *handler) Initialize(s courier.Server) error {
	h.SetServer(s)
	s.AddHandlerRoute(h, http.MethodPost, "receive", handlers.NewTelReceiveHandler(&h.BaseHandler, "from", "body"))
	return nil
}

// {
// 	"messages": [
// 	  {
// 		"to": "+61411111111",
// 		"source": "sdk",
// 		"body": "body"
// 	  },
// 	  {
// 		"list_id": 0,
// 		"source": "sdk",
// 		"body": "body"
// 	  }
// 	]
// }
type mtPayload struct {
	Messages [1]struct {
		To     string `json:"to"`
		From   string `json:"from"`
		Body   string `json:"body"`
		Source string `json:"source"`
	} `json:"messages"`
}

// Send sends the given message, logging any HTTP calls or errors
func (h *handler) Send(ctx context.Context, msg courier.Msg, logger *courier.ChannelLogger) (courier.MsgStatus, error) {
	username := msg.Channel().StringConfigForKey(courier.ConfigUsername, "")
	if username == "" {
		return nil, fmt.Errorf("Missing 'username' config for CS channel")
	}

	password := msg.Channel().StringConfigForKey(courier.ConfigPassword, "")
	if password == "" {
		return nil, fmt.Errorf("Missing 'password' config for CS channel")
	}

	status := h.Backend().NewMsgStatusForID(msg.Channel(), msg.ID(), courier.MsgErrored)
	parts := handlers.SplitMsgByChannel(msg.Channel(), handlers.GetTextAndAttachments(msg), maxMsgLength)
	for _, part := range parts {
		payload := &mtPayload{}
		payload.Messages[0].To = msg.URN().Path()
		payload.Messages[0].From = msg.Channel().Address()
		payload.Messages[0].Body = part
		payload.Messages[0].Source = "courier"

		requestBody := &bytes.Buffer{}
		json.NewEncoder(requestBody).Encode(payload)

		// build our request
		req, err := http.NewRequest(http.MethodPost, sendURL, requestBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.SetBasicAuth(username, password)

		resp, respBody, err := handlers.RequestHTTP(req, logger)
		if err != nil || resp.StatusCode/100 != 2 {
			return status, nil
		}

		// first read our status
		s, err := jsonparser.GetString(respBody, "data", "messages", "[0]", "status")
		if s != "SUCCESS" {
			logger.Error(errors.Errorf("received non SUCCESS status: %s", s))
			return status, nil
		}

		// then get our external id
		id, err := jsonparser.GetString(respBody, "data", "messages", "[0]", "message_id")
		if err != nil {
			logger.Error(errors.Errorf("unable to get message_id for message"))
			return status, nil
		}

		status.SetExternalID(id)
		status.SetStatus(courier.MsgWired)
	}

	return status, nil
}
