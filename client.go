package slacktest

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	gonanoid "github.com/matoous/go-nanoid"
	"github.com/slack-go/slack"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	TeamId = "test_team_id"
)

type User interface {
	Page
	HomeOpen(t *testing.T) Page
}

type Trigger interface {
	ShowOpenModal() Page
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

func (m *MessageView) WaitUpdate(t *testing.T) {
	select {
	case <-m.slackMessage.update:
		m.page.set(m.slackMessage.slackMessage.Blocks)
	case <-time.After(time.Second * 5):
		t.Fatal("cannot wait update for message")
	}
}

type Messages []MessageView

func (m Messages) Last() *MessageView {
	if len(m) == 0 {
		return nil
	}
	return &m[len(m)-1]
}

type Client struct {
	activeModal *slack.ModalViewRequest

	eventUrl string

	appClient *AppHttpClient

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
		router := NewMock(c)

		if err := http.ListenAndServe(port, router); err != nil {
			panic(err)
		}
	}()

	return nil
}

func (c *Client) User(t *testing.T, id string) User {
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

			resp, err := c.appClient.SendInteractionAction(&event)
			if err != nil {
				t.Fatal(err)
				return
			}

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
				if resp != nil {
					switch resp.ResponseAction {
					case slack.RAUpdate:
						uc.viewsStack[len(uc.viewsStack)-1] = resp.View
						uc.page.set(resp.View.Blocks)
					}
				} else {
					uc.viewsStack = uc.viewsStack[:len(uc.viewsStack)-1]
					if len(uc.viewsStack) == 0 {
						if uc.home != nil {
							uc.page.set(uc.home.Blocks)
						}
					} else {
						uc.page.set(uc.viewsStack[len(uc.viewsStack)-1].Blocks)
					}
				}
			}
		},
	}

	uc.page = page

	return &uc
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

func (c *AppHttpClient) SendInteractionAction(event *slack.InteractionCallback) (*slack.ViewSubmissionResponse, error) {
	b, err := json.Marshal(event)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status code not 200, is %v", res.StatusCode)
	}

	bResp, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var viewResponse slack.ViewSubmissionResponse

	if err := json.Unmarshal(bResp, &viewResponse); err != nil {
		return nil, nil
	}

	if viewResponse.ResponseAction == "" {
		return nil, nil
	}

	return &viewResponse, nil
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
		return fmt.Errorf("status code not 200, is %v", res.StatusCode)
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
