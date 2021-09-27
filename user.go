package slacktest

import (
	"encoding/json"
	gonanoid "github.com/matoous/go-nanoid"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"testing"
	"time"
)

type userClient struct {
	page
	user         *slack.User
	userId       string
	client       *AppHttpClient
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

func (a *userClient) HomeOpen(t *testing.T) Page {
	innerEventBytes, err := json.Marshal(slackevents.AppHomeOpenedEvent{
		Type:           slackevents.AppHomeOpened,
		User:           a.userId,
		Channel:        "",
		EventTimeStamp: "",
		Tab:            "home",
		View:           slack.View{},
	})
	if err != nil {
		t.Fatal(err)
		return nil
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
		t.Fatal(err)
		return nil
	case v := <-a.pageUpdate:
		a.home = v
		a.currentPage = v.Blocks
		a.page.set(v.Blocks)
	case <-time.After(time.Second * 5):
		t.Fatal("wait home after 5 seconds")
	}

	return nil
}
