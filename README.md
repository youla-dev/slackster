# Slackster

Library for testing interactive Slack applications.

* Mock Slack API: user info, post and update message, publish view.
* Testing Slack UI in the home tab or in message blocks (button/input/etc.). No dependency on Slack API.
* Integration with GO testing library.

```go
func TestSimple(t *testing.T) {
	teamId := fmt.Sprintf("%v", time.Now().UnixNano())
	client := slackster.NewClient("http://localhost:4000/api/slack/events", "http://localhost:4000/api/slack/actions", "your_secret_key", teamId)
	if err := client.Start(":4999"); err != nil {
		panic(err)
	}

	client.RegisterUser(&slack.User{
		ID:      "first",
		Name:    "First First",
		IsAdmin: true,
	})

	client.RegisterUser(&slack.User{
		ID:      "second",
		Name:    "Second Second",
		IsAdmin: false,
	})

	user := client.User(t, "first")

	user.HomeOpen(t)
	user.ClickByText(t, "Start button", true) // true for wait modal
	user.SelectUserByText(t, "User select", "")
	user.Type(t, "Input with label", "hello man") // type input text
	user.SubmitForm()
	user.WaitHomeUpdate()

	secondUser := client.User(t, "second")

	lastMessage := secondUser.Messages().Last()
	assert.NotNil(t, lastMessage)

	lastMessage.SearchByText(t, "hello man") // search text in received messages
}
```

And use http://localhost:4999 for fully mock Slack API

```go
package main

import (
	"github.com/slack-go/slack"
)

func main() {
	api := slack.New("YOUR_TOKEN_HERE", slack.OptionAPIURL("http://localhost:4999"))
}
```

## Available methods

| Method                                                                                    | Description                                                                          |
|-------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| ClickByText(t *testing.T, text string, waitModal bool)                          | Find button with text and click on it. And wait modal if need be.                    |
| SearchByText(t *testing.T, text string) interface{}                              | Find element with text, and return slack element (https://github.com/slack-go/slack) |
| Type(t *testing.T, searchText string, value string)  interface{}               | Find input with text, and type value.                                                |
| SubmitForm()                                                                              | Submit view form.                                                                    |
| WaitHomeUpdate()                                                                          | Wait any home update (publish view)                                                  |
| ClickByActionId(t *testing.T, actionId string, value string, waitModal bool) | Find button by action, and click on it.                                              |
| SelectUserByText (t *testing.T, text string, user string)                        | Find user select by text in placeholder, and select user.                            |
| SelectUsersByText (t *testing.T, text string, users []string)                    | Find multi user select by text in placeholder, and select users.                     |
| SelectByText (t *testing.T, searchText string, value string)                     | Find select by text in placeholder, and select option by value                       |
| Messages() Messages                                                                     | Get a message for the user. Any message is a page with all the above methods         |