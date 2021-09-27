package slacktest

import (
	"encoding/json"
	"fmt"
	"github.com/slack-go/slack"
	"testing"
	"time"
)

type Page interface {
	ClickByText(t *testing.T, text string, waitModal bool)
	SearchByText(t *testing.T, text string) interface{}
	Type(t *testing.T, searchText string, value string) interface{}
	Element(actionId string) Element
	SubmitForm()
	WaitHomeUpdate()
	ClickByActionId(t *testing.T, actionId string, value string, waitModal bool)
	SelectUserByText(t *testing.T, text string, user string)
	SelectUsersByText(t *testing.T, text string, users []string)
	SelectByText(t *testing.T, searchText string, value string)
	Wait(duration time.Duration)
	Messages() Messages
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

func (p *page) SelectByText(t *testing.T, searchText string, value string) {
	el := p.SearchByText(t, searchText)

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
				t.Fatal("value not found in select")
				return
			}

			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{
				SelectedOption: slack.OptionBlockObject{
					Value: finalValue,
				},
			}
		}
	}
}

func (p *page) SelectUsersByText(t *testing.T, text string, users []string) {
	el := p.SearchByText(t, text)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.MultiSelectBlockElement:
			if blockElement.Type != slack.MultiOptTypeUser {
				t.Fatal("input is not single user")
				return
			}
			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{SelectedUsers: users}
		default:
			t.Fatal("input is not user selector")
			return
		}
	}
}

func (p *page) SelectUserByText(t *testing.T, text string, user string) {
	el := p.SearchByText(t, text)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.SelectBlockElement:
			if blockElement.Type != slack.OptTypeUser {
				t.Fatal(fmt.Sprintf("input is not single user, is %s", blockElement.Type))
				return
			}
			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{SelectedUser: user}
		default:
			t.Fatal("input is not user selector")
			return
		}
	}
}

func (p *page) Type(t *testing.T, searchText string, value string) interface{} {
	el := p.SearchByText(t, searchText)

	if x, ok := el.(*slack.InputBlock); ok {
		p.state[x.BlockID] = map[string]slack.BlockAction{}
		switch blockElement := x.Element.(type) {
		case *slack.PlainTextInputBlockElement:
			p.state[x.BlockID][blockElement.ActionID] = slack.BlockAction{Value: value}
		}
	}

	return el
}

func (p *page) ClickByActionId(t *testing.T, actionId string, value string, waitModal bool) {
	res := blocks(p.page).SearchByActionIdAndValue(actionId, value)
	if res == nil {
		t.Fatalf("cannot search element with action=%s&value=%s", actionId, value)
	}

	switch x := res.(type) {
	case *slack.ButtonBlockElement:
		p.actionCallback(x.ActionID, slack.InteractionTypeBlockActions, waitModal, p.state, x.Value)
	default:
		t.Fatal("cannot click by element")
	}
}

func (p *page) ClickByText(t *testing.T, text string, waitModal bool) {
	res := blocks(p.page).SearchByText(text)
	if res == nil {
		t.Fatal(fmt.Sprintf("cannot search element with text=%s", text))
		return
	}

	switch x := res.(type) {
	case *slack.ButtonBlockElement:
		p.actionCallback(x.ActionID, slack.InteractionTypeBlockActions, waitModal, p.state, x.Value)
	default:
		t.Fatal("cannot click by element")
		return
	}

	return
}

func (p *page) SubmitForm() {
	p.actionCallback("", slack.InteractionTypeViewSubmission, false, p.state, "")
}

func (p *page) SearchByText(t *testing.T, text string) interface{} {
	res := blocks(p.page).SearchByText(text)
	if res == nil {
		t.Fatalf("cannot search element with text=%s", text)
		return nil
	}

	return res
}

func (p *page) Element(actionId string) Element {
	panic("implement me")
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

			if x.Accessory != nil {
				var blockElement slack.BlockElement

				if x.Accessory.ButtonElement != nil {
					blockElement = x.Accessory.ButtonElement
				}

				res := searchInBlockElements(&slack.BlockElements{
					ElementSet: []slack.BlockElement{blockElement},
				}, text)
				if res != nil {
					return res
				}
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
