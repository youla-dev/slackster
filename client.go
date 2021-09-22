package slacktest

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	gonanoid "github.com/matoous/go-nanoid"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	TeamId = "test_team_id"
)

type client interface {
	User(id string) User
	MessagesByUser(id string) []*slack.Msg
}

type User interface {
	Page
	HomeOpen() Page
}

type Trigger interface {
	ShowOpenModal() Page
}

type Page interface {
	Click(actionId string) Trigger
	ClickByText(text string, waitModal bool)
	SearchByText(text string) interface{}
	Type(searchText string, value string) interface{}
	Element(actionId string) Element
	SubmitForm()
	WaitHomeUpdate()
	ClickByActionId(actionId string, value string, waitModal bool)
	SelectUserByText(text string, user string)
	SelectUsersByText(text string, users []string)
	SelectByText(searchText string, value string)
	Wait(duration time.Duration)
	Messages() Messages
}

type Element interface {
	Visible()
}

type messages struct {
	List []*message
}

func (m *messages) Last() *message {
	return m.List[len(m.List)-1]
}

type message struct {
	page
	slackMessage *slack.Msg
	update       chan struct{}
}

type MessageView struct {
	page
	slackMessage *message
}

func (m *MessageView) WaitUpdate() {
	select {
	case <-m.slackMessage.update:
		m.page.set(m.slackMessage.slackMessage.Blocks)
	case <-time.After(time.Second * 5):
		panic("cannot wait update for message")
	}
}

type Messages []MessageView

func (m Messages) Last() *MessageView {
	return &m[len(m)-1]
}

type Client struct {
	activeModal *slack.ModalViewRequest

	eventUrl string

	appClient AppClient

	users           map[string]*slack.User
	pagesByTriggers map[string]chan *slack.ModalViewRequest
	viewsByTrigger  map[string]chan *slack.ModalViewRequest
	messagesByUser  map[string]*messages
	teamId          string
	port            string
}

func NewClient(eventUrl string, interactionUrl string, signedSecret string, teamId string) *Client {
	if teamId == "" {
		teamId = TeamId
	}

	return &Client{
		eventUrl:        eventUrl,
		appClient:       NewAppHttpClient(eventUrl, interactionUrl, signedSecret, teamId),
		users:           map[string]*slack.User{},
		pagesByTriggers: map[string]chan *slack.ModalViewRequest{},
		viewsByTrigger:  make(map[string]chan *slack.ModalViewRequest, 1),
		teamId:          teamId,
		messagesByUser:  map[string]*messages{},
	}
}

func (c *Client) MessagesByUser(id string) *messages {
	return c.messagesByUser[id]
}

func (c *Client) Team(id string) {
	c.teamId = id
}

func (c *Client) RegisterUser(user *slack.User) {
	c.users[user.ID] = user
}

type SlackResp struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error"`

	User *slack.User `json:"user,omitempty"`
}

func (c *Client) Start(port string) error {
	c.port = port
	go func() {
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

			fmt.Println("user info response", string(b))

			w.Write(b)
		})

		if err := http.ListenAndServe(port, router); err != nil {
			panic(err)
		}
	}()

	return nil
}

func (c *Client) User(id string) User {
	ch := make(chan *slack.ModalViewRequest, 1)

	c.pagesByTriggers[id] = ch

	uc := userClient{
		userId:     id,
		client:     c.appClient,
		pageUpdate: ch,
		teamId:     c.teamId,
		messages:   c.messagesByUser[id],
		_client:    c,
		user:       c.users[id],
	}

	page := page{
		state: map[string]map[string]slack.BlockAction{},
		actionCallback: func(actionId string, typ slack.InteractionType, waitModal bool, state map[string]map[string]slack.BlockAction, value string) {
			triggerId, _ := gonanoid.Nanoid()
			c.viewsByTrigger[triggerId] = make(chan *slack.ModalViewRequest, 1)

			var view slack.View

			if len(uc.viewsStack) > 0 {
				view = slack.View{
					PrivateMetadata: uc.viewsStack[len(uc.viewsStack)-1].PrivateMetadata,
					State: &slack.ViewState{
						Values: state,
					},
				}
			}

			event := slack.InteractionCallback{
				Type:        typ,
				Token:       "",
				CallbackID:  "",
				ResponseURL: "",
				TriggerID:   triggerId,
				ActionTs:    "",
				Team: slack.Team{
					ID: c.teamId,
				},
				Channel:                  slack.Channel{},
				User:                     *c.users[id],
				OriginalMessage:          slack.Message{},
				Message:                  slack.Message{},
				Name:                     "",
				Value:                    "",
				MessageTs:                "",
				AttachmentID:             "",
				ActionCallback:           slack.ActionCallbacks{},
				View:                     view,
				ActionID:                 "",
				APIAppID:                 "",
				BlockID:                  "",
				Container:                slack.Container{},
				DialogSubmissionCallback: slack.DialogSubmissionCallback{},
				ViewSubmissionCallback:   slack.ViewSubmissionCallback{},
				ViewClosedCallback:       slack.ViewClosedCallback{},
				RawState:                 nil,
			}

			event.ActionCallback.BlockActions = append(event.ActionCallback.BlockActions, &slack.BlockAction{
				ActionID: actionId,
				Value:    value,
			})
			c.appClient.SendInteractionAction(&event)
			if waitModal {
				view := <-c.viewsByTrigger[triggerId]
				uc.currentModal = view
				uc.viewsStack = append(uc.viewsStack, view)
				uc.set(view.Blocks)
			} else {
				go func() {
					view := <-c.viewsByTrigger[triggerId]
					uc.currentModal = view
					uc.viewsStack = append(uc.viewsStack, view)
					uc.set(view.Blocks)
				}()
			}

			// TODO form maybe error
			if event.Type == slack.InteractionTypeViewSubmission {
				uc.viewsStack = uc.viewsStack[:len(uc.viewsStack)-1]
				if len(uc.viewsStack) == 0 {
					if uc.home != nil {
						uc.page.set(uc.home.Blocks)
					}
				} else {
					uc.page.set(uc.viewsStack[len(uc.viewsStack)-1].Blocks)
				}
			}
		},
	}

	uc.page = page

	return &uc
}

type page struct {
	page           slack.Blocks
	raw            string
	actionCallback func(actionId string, typ slack.InteractionType, waitModal bool, state map[string]map[string]slack.BlockAction, value string)
	state          map[string]map[string]slack.BlockAction
}

func (p *page) set(block slack.Blocks) {
	p.page = block
	b, _ := json.Marshal(block)

	p.raw = string(b)
}

func (p *page) SelectByText(searchText string, value string) {
	el := p.SearchByText(searchText)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.SelectBlockElement:
			var hasValue bool
			var finalValue string

			for _, option := range blockElement.Options {
				if option.Text.Text == value {
					hasValue = true
					finalValue = option.Value
					break
				}
			}

			if !hasValue {
				panic("value not found in select")
			}

			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{
				SelectedOption: slack.OptionBlockObject{
					Value: finalValue,
				},
			}
		}
	}
}

func (p *page) SelectUsersByText(text string, users []string) {
	el := p.SearchByText(text)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.MultiSelectBlockElement:
			if blockElement.Type != slack.MultiOptTypeUser {
				panic("input is not single user")
			}
			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{SelectedUsers: users}
		default:
			panic("input is not user selector")
		}
	}
}

func (p *page) SelectUserByText(text string, user string) {
	el := p.SearchByText(text)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.SelectBlockElement:
			if blockElement.Type != slack.OptTypeUser {
				panic(fmt.Sprintf("input is not single user, is %s", blockElement.Type))
			}
			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{SelectedUser: user}
		default:
			panic("input is not user selector")
		}
	}
}

func (p *page) Type(searchText string, value string) interface{} {
	el := p.SearchByText(searchText)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.PlainTextInputBlockElement:
			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{Value: value}
		}
	}

	return el
}

func (p *page) ClickByActionId(actionId string, value string, waitModal bool) {
	res := blocks(p.page).SearchByActionIdAndValue(actionId, value)
	if res == nil {
		panic(fmt.Sprintf("cannot search element with action=%s&value=%s", actionId, value))
	}

	switch x := res.(type) {
	case *slack.ButtonBlockElement:
		p.actionCallback(x.ActionID, slack.InteractionTypeBlockActions, waitModal, p.state, x.Value)
	default:
		panic("cannot click by element")
	}
}

func (p *page) ClickByText(text string, waitModal bool) {
	res := blocks(p.page).SearchByText(text)
	if res == nil {
		panic(fmt.Sprintf("cannot search element with text=%s", text))
	}

	switch x := res.(type) {
	case *slack.ButtonBlockElement:
		p.actionCallback(x.ActionID, slack.InteractionTypeBlockActions, waitModal, p.state, x.Value)
	default:
		panic("cannot click by element")
	}

	return
}

func (p *page) SubmitForm() {
	p.actionCallback("", slack.InteractionTypeViewSubmission, false, p.state, "")
}

func (p *page) SearchByText(text string) interface{} {
	res := blocks(p.page).SearchByText(text)
	if res == nil {
		panic(fmt.Sprintf("cannot search element with text=%s", text))
	}

	return res
}

func (p *page) Click(actionId string) Trigger {
	panic("implement me")
}

func (p *page) Element(actionId string) Element {
	panic("implement me")
}

type userClient struct {
	page
	user         *slack.User
	userId       string
	client       AppClient
	pageUpdate   <-chan *slack.ModalViewRequest
	currentPage  slack.Blocks
	currentModal *slack.ModalViewRequest
	home         *slack.ModalViewRequest
	viewsStack   []*slack.ModalViewRequest
	teamId       string
	messages     *messages
	_client      *Client
}

func (a *userClient) Messages() Messages {
	var messagesWithView []MessageView

	uc := a

	for _, msg := range a.messages.List {
		messagesWithView = append(messagesWithView, MessageView{
			slackMessage: msg,
			page: page{
				state: map[string]map[string]slack.BlockAction{},
				page:  msg.slackMessage.Blocks,
				actionCallback: func(actionId string, typ slack.InteractionType, waitModal bool, state map[string]map[string]slack.BlockAction, value string) {
					triggerId, _ := gonanoid.Nanoid()
					a._client.viewsByTrigger[triggerId] = make(chan *slack.ModalViewRequest, 1)

					var view slack.View

					if len(uc.viewsStack) > 0 {
						view = slack.View{
							PrivateMetadata: uc.viewsStack[len(uc.viewsStack)-1].PrivateMetadata,
							State: &slack.ViewState{
								Values: state,
							},
						}
					}

					event := slack.InteractionCallback{
						Type:        typ,
						Token:       "",
						CallbackID:  "",
						ResponseURL: "http://localhost" + a._client.port + "/api/response_url/" + msg.slackMessage.Channel + "/" + msg.slackMessage.Timestamp,
						TriggerID:   triggerId,
						ActionTs:    "",
						Team: slack.Team{
							ID: a.teamId,
						},
						Channel:                  slack.Channel{},
						User:                     *a.user,
						OriginalMessage:          slack.Message{},
						Message:                  slack.Message{},
						Name:                     "",
						Value:                    "",
						MessageTs:                "",
						AttachmentID:             "",
						ActionCallback:           slack.ActionCallbacks{},
						View:                     view,
						ActionID:                 "",
						APIAppID:                 "",
						BlockID:                  "",
						Container:                slack.Container{},
						DialogSubmissionCallback: slack.DialogSubmissionCallback{},
						ViewSubmissionCallback:   slack.ViewSubmissionCallback{},
						ViewClosedCallback:       slack.ViewClosedCallback{},
						RawState:                 nil,
					}

					event.ActionCallback.BlockActions = append(event.ActionCallback.BlockActions, &slack.BlockAction{
						ActionID: actionId,
						Value:    value,
					})

					a._client.appClient.SendInteractionAction(&event)
					if waitModal {
						view := <-a._client.viewsByTrigger[triggerId]
						uc.currentModal = view
						uc.viewsStack = append(uc.viewsStack, view)
						uc.set(view.Blocks)
					} else {
						go func() {
							view := <-a._client.viewsByTrigger[triggerId]
							uc.currentModal = view
							uc.viewsStack = append(uc.viewsStack, view)
							uc.set(view.Blocks)
						}()
					}

					// TODO form maybe error
					if event.Type == slack.InteractionTypeViewSubmission {
						uc.viewsStack = uc.viewsStack[:len(uc.viewsStack)-1]
						if len(uc.viewsStack) == 0 {
							uc.page.set(uc.home.Blocks)
						} else {
							uc.page.set(uc.viewsStack[len(uc.viewsStack)-1].Blocks)
						}
					}
				},
			},
		})
	}

	return messagesWithView
}

func (a *userClient) WaitHomeUpdate() {
	v := <-a.pageUpdate
	a.home = v
	a.page.set(v.Blocks)
}

func (a *userClient) Wait(duration time.Duration) {
	<-time.After(duration)
}

func (a *userClient) HomeOpen() Page {
	innerEventBytes, err := json.Marshal(slackevents.AppHomeOpenedEvent{
		Type:           slackevents.AppHomeOpened,
		User:           a.userId,
		Channel:        "",
		EventTimeStamp: "",
		Tab:            "home",
		View:           slack.View{},
	})
	if err != nil {
		panic(err)
	}

	innerEvent := json.RawMessage(innerEventBytes)

	event := &slackevents.EventsAPICallbackEvent{
		Type:       slackevents.CallbackEvent,
		InnerEvent: &innerEvent,
		TeamID:     a.teamId,
	}

	errCh := make(chan error)

	go func() {
		if err := a.client.PushEvent(&event); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		panic(err)
		return nil
	case v := <-a.pageUpdate:
		a.home = v
		a.currentPage = v.Blocks
		a.page.set(v.Blocks)
	case <-time.After(time.Second * 5):
		panic("wait home after 5 seconds")
	}

	return nil
}

type AppClient interface {
	PushEvent(event interface{}) error
	SendAction(actionId string, typ slack.InteractionType, user *slack.User, triggerId string, state map[string]map[string]slack.BlockAction) (string, error)
	SendInteractionAction(event *slack.InteractionCallback) error
}

type AppHttpClient struct {
	eventUrl       string
	interactionUrl string
	client         *http.Client
	signed         string
	teamId         string
}

func NewAppHttpClient(eventUrl string, interactionUrl string, signedSecret string, teamId string) *AppHttpClient {
	return &AppHttpClient{eventUrl: eventUrl, interactionUrl: interactionUrl, client: &http.Client{}, teamId: teamId, signed: signedSecret}
}

func (c *AppHttpClient) SendInteractionAction(event *slack.InteractionCallback) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f := url.Values{}
	f.Set("payload", string(b))

	req, _ := http.NewRequest("POST", c.interactionUrl, strings.NewReader(f.Encode()))

	secret, timestamp := generateSecret(c.signed, f.Encode())

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Signature", secret)
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		panic(fmt.Sprintf("status code not 200, is %v", res.StatusCode))
	}

	return nil
}

func (c *AppHttpClient) SendAction(actionId string, typ slack.InteractionType, user *slack.User, triggerId string, state map[string]map[string]slack.BlockAction) (string, error) {
	event := slack.InteractionCallback{
		Type:        typ,
		Token:       "",
		CallbackID:  "",
		ResponseURL: "",
		TriggerID:   triggerId,
		ActionTs:    "",
		Team: slack.Team{
			ID: c.teamId,
		},
		Channel:                  slack.Channel{},
		User:                     *user,
		OriginalMessage:          slack.Message{},
		Message:                  slack.Message{},
		Name:                     "",
		Value:                    "",
		MessageTs:                "",
		AttachmentID:             "",
		ActionCallback:           slack.ActionCallbacks{},
		View:                     slack.View{},
		ActionID:                 "",
		APIAppID:                 "",
		BlockID:                  "",
		Container:                slack.Container{},
		DialogSubmissionCallback: slack.DialogSubmissionCallback{},
		ViewSubmissionCallback:   slack.ViewSubmissionCallback{},
		ViewClosedCallback:       slack.ViewClosedCallback{},
		RawState:                 nil,
		BlockActionState:         nil,
	}

	event.ActionCallback.BlockActions = append(event.ActionCallback.BlockActions, &slack.BlockAction{
		ActionID: actionId,
	})

	b, err := json.Marshal(event)
	if err != nil {
		return "", err
	}

	f := url.Values{}
	f.Set("payload", string(b))

	req, _ := http.NewRequest("POST", c.interactionUrl, strings.NewReader(f.Encode()))

	secret, timestamp := generateSecret(c.signed, f.Encode())

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Signature", secret)
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)

	res, err := c.client.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		panic(fmt.Sprintf("status code not 200, is %v", res.StatusCode))
	}

	return event.TriggerID, nil
}

func (c *AppHttpClient) PushEvent(event interface{}) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest("POST", c.eventUrl, bytes.NewReader(b))

	secret, timestamp := generateSecret(c.signed, string(b))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Signature", secret)
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		panic(fmt.Sprintf("status code not 200, is %v", res.StatusCode))
	}

	return nil
}

func generateSecret(secret string, body string) (string, string) {
	timestamp := fmt.Sprintf("%v", time.Now().Unix())
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(fmt.Sprintf("v0:%s:%s", timestamp, body)))

	// Get result and encode as hexadecimal string
	sha := hex.EncodeToString(h.Sum(nil))

	return "v0=" + sha, timestamp
}

type blocks slack.Blocks

func (b blocks) SearchByText(text string) interface{} {
	for _, block := range b.BlockSet {
		switch x := block.(type) {
		case *slack.DividerBlock:
		case *slack.ActionBlock:
			res := searchInBlockElements(x.Elements, text)
			if res != nil {
				return res
			}
		case *slack.InputBlock:
			res := searchInBlockElements(&slack.BlockElements{ElementSet: []slack.BlockElement{x.Element}}, text)
			if res != nil {
				return x
			}
		case *slack.HeaderBlock:
			if x.Text.Text == text {
				return x
			}
		case *slack.SectionBlock:
			if x.Text.Text == text {
				return x
			}
		}
	}

	return nil
}

func searchInBlockElements(elements *slack.BlockElements, text string) interface{} {
	for _, el := range elements.ElementSet {
		switch x := el.(type) {
		case *slack.ButtonBlockElement:
			if x.Text.Text == text {
				return x
			}
		case *slack.PlainTextInputBlockElement:
			if x.Placeholder.Text == text {
				return x
			}
		case *slack.SelectBlockElement:
			if x.Placeholder.Text == text {
				return x
			}
		case *slack.MultiSelectBlockElement:
			if x.Placeholder.Text == text {
				return x
			}
		}
	}

	return nil
}

func (b blocks) SearchByActionIdAndValue(actionId string, value string) interface{} {
	for _, block := range b.BlockSet {
		switch x := block.(type) {
		case *slack.DividerBlock:
		case *slack.ActionBlock:
			res := searchInBlockElementsByActionIdAndValues(x.Elements, actionId, value)
			if res != nil {
				return res
			}
		case *slack.InputBlock:
			res := searchInBlockElementsByActionIdAndValues(&slack.BlockElements{ElementSet: []slack.BlockElement{x.Element}}, actionId, value)
			if res != nil {
				return x
			}
		}
	}

	return nil
}

func searchInBlockElementsByActionIdAndValues(elements *slack.BlockElements, actionId string, value string) interface{} {
	for _, el := range elements.ElementSet {
		switch x := el.(type) {
		case *slack.ButtonBlockElement:
			if x.ActionID == actionId {
				if value == "" {
					return x
				}

				if value == x.Value {
					return x
				}
			}
		}
	}

	return nil
}
