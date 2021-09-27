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