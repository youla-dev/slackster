package slacktest

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/slack-go/slack"
	"io/ioutil"
	"net/http"
	"time"
)

func NewMock(c *Client) http.Handler {
	router := chi.NewRouter()

	router.Use(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
			fmt.Println(req.URL.String(), req.Method)
			handler.ServeHTTP(writer, req)
		})
	})

	router.Post("/api/response_url/{channel}/{ts}", func(w http.ResponseWriter, req *http.Request) {
		channel := chi.URLParam(req, "channel")
		ts := chi.URLParam(req, "ts")

		inMessage := slack.Msg{
			Channel:   channel,
			Timestamp: ts,
		}

		reqBytes, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		if err := json.Unmarshal(reqBytes, &inMessage); err != nil {
			w.WriteHeader(500)
			return
		}

		inMessage.Channel = channel
		inMessage.Timestamp = ts

		userMessages := c.messagesByUser[inMessage.Channel]

		for _, msg := range userMessages.List {
			if msg.slackMessage.Timestamp == inMessage.Timestamp {
				msg.slackMessage.Blocks = inMessage.Blocks
				go func() {
					msg.update <- struct{}{}
				}()
			}
		}

		res := struct {
			slack.SlackResponse
			Channel string `json:"channel"`
			Ts      string `json:"ts"`
		}{
			SlackResponse: slack.SlackResponse{
				Ok: true,
			},
			Channel: inMessage.Channel,
			Ts:      inMessage.Timestamp,
		}

		w.Header().Set("Content-Type", "application/json")

		b, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		w.Write(b)
	})

	router.Post("/api/chat.update", func(w http.ResponseWriter, req *http.Request) {
		req.ParseForm()

		inMessage := slack.Msg{
			Channel:   req.Form.Get("channel"),
			Timestamp: req.Form.Get("ts"),
		}

		if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &inMessage.Blocks); err != nil {
			w.WriteHeader(500)
			return
		}

		userMessages := c.messagesByUser[inMessage.Channel]

		for _, msg := range userMessages.List {
			currentMsg := msg
			if msg.slackMessage.Timestamp == inMessage.Timestamp {
				currentMsg.slackMessage.Blocks = inMessage.Blocks
				go func() {
					currentMsg.update <- struct{}{}
				}()
			}
		}

		res := struct {
			slack.SlackResponse
			Channel string `json:"channel"`
			Ts      string `json:"ts"`
		}{
			SlackResponse: slack.SlackResponse{
				Ok: true,
			},
			Channel: inMessage.Channel,
			Ts:      inMessage.Timestamp,
		}

		b, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		w.Write(b)
	})

	router.Post("/api/chat.postMessage", func(w http.ResponseWriter, req *http.Request) {
		req.ParseForm()

		inMessage := slack.Msg{
			Channel:   req.Form.Get("channel"),
			Timestamp: fmt.Sprintf("%v", time.Now().UnixNano()),
		}

		if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &inMessage.Blocks); err != nil {
			w.WriteHeader(500)
			return
		}

		userMessages := c.messagesByUser[inMessage.Channel]
		if userMessages == nil {
			userMessages = &messages{}
			c.messagesByUser[inMessage.Channel] = userMessages
		}

		_msg := message{
			page: page{
				state: map[string]map[string]slack.BlockAction{},
			},
			slackMessage: &inMessage,
			update:       make(chan struct{}),
		}

		_msg.page.set(inMessage.Blocks)

		userMessages.List = append(userMessages.List, &_msg)

		res := struct {
			slack.SlackResponse
			Channel string `json:"channel"`
			Ts      string `json:"ts"`
		}{
			SlackResponse: slack.SlackResponse{
				Ok: true,
			},
			Channel: inMessage.Channel,
			Ts:      inMessage.Timestamp,
		}

		b, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		w.Write(b)
	})

	router.Post("/api/views.publish", func(w http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()

		b, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		request := struct {
			UserID string                 `json:"user_id"`
			View   slack.ModalViewRequest `json:"view"`
			Hash   string                 `json:"hash,omitempty"`
		}{}

		if err := json.Unmarshal(b, &request); err != nil {
			w.WriteHeader(500)
			return
		}

		c.pagesByTriggers[request.UserID] <- &request.View

		resp, err := json.Marshal(slack.ViewResponse{
			SlackResponse: slack.SlackResponse{
				Ok: true,
			},
		})
		if err != nil {
			w.WriteHeader(500)
			return
		}

		w.Write(resp)
	})

	router.Post("/api/views.open", func(w http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()

		b, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(500)
			return
		}

		reqBody := struct {
			TriggerID string                 `json:"trigger_id"`
			View      slack.ModalViewRequest `json:"view"`
		}{}

		if err := json.Unmarshal(b, &reqBody); err != nil {
			w.WriteHeader(500)
			return
		}

		c.viewsByTrigger[reqBody.TriggerID] <- &reqBody.View

		resB, err := json.Marshal(slack.ViewResponse{
			SlackResponse: slack.SlackResponse{
				Ok: true,
			},
		})
		if err != nil {
			w.WriteHeader(500)
			return
		}

		w.Write(resB)
	})

	router.Post("/api/users.info", func(w http.ResponseWriter, req *http.Request) {
		req.ParseForm()

		id := req.Form.Get("user")

		fmt.Println("request info for", id)

		user, ok := c.users[id]
		if !ok {
			w.WriteHeader(404)
			return
		}

		b, err := json.Marshal(SlackResp{
			Ok:   true,
			User: user,
		})
		if err != nil {
			fmt.Println(err)
			return
		}

		w.Write(b)
	})

	return router
}
