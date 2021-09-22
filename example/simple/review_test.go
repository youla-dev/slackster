package simple

import (
	"fmt"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"slacktest"
	"testing"
	"time"
)

func TestReview(t *testing.T) {
	teamId := fmt.Sprintf("%v", time.Now().UnixNano())
	client := slacktest.NewClient("http://localhost:4000/api/slack/events", "http://localhost:4000/api/slack/actions", "847c94fba6f3caff5f5dafa9b23aaffb", teamId)
	if err := client.Start(":4999"); err != nil {
		panic(err)
	}

	client.RegisterUser(&slack.User{
		ID:      "first",
		Name:    "First First",
		IsAdmin: true,
	})
	client.RegisterUser(&slack.User{
		ID:   "second",
		Name: "Second Second",
	})
	client.RegisterUser(&slack.User{
		ID:   "third",
		Name: "Mister Third",
	})

	user := client.User("first")

	t.Run("create review", func(t *testing.T) {
		reviewName := fmt.Sprintf("%v", time.Now().UnixNano())

		user.HomeOpen()
		user.ClickByText("Новое ревью :pencil2:", true)
		user.Type("Название ревью", reviewName)
		user.SubmitForm()
		user.WaitHomeUpdate()
		user.SearchByText(reviewName)
	})

	t.Run("add members", func(t *testing.T) {
		user.ClickByActionId("add_member", "", true)
		user.SelectUserByText("Выбрать менеджера", "first")
		user.SelectUsersByText("Выбрать сотрудника", []string{"second", "third"})
		user.SubmitForm()
		user.WaitHomeUpdate()

		user.SearchByText("Менеджер <@first>, сотрудники: <@second>,<@third>")
	})

	t.Run("start review", func(t *testing.T) {
		user.ClickByActionId("start_review", "", false)
		user.WaitHomeUpdate() // first update for status
		user.SearchByText(fmt.Sprintf("Ревью *в процессе запуска*.\nВсего разослано форм: %v\nФорм на стадии заполнения %v\nФорм на стадии аппрува %v\nФорм на стадии peer review %v\nФорм завершенно %v\nФорм с отчётами %v", 0, 0, 0, 0, 0, 0))
		user.WaitHomeUpdate()
		user.SearchByText(fmt.Sprintf("Ревью *запущено*.\nВсего разослано форм: %v\nФорм на стадии заполнения %v\nФорм на стадии аппрува %v\nФорм на стадии peer review %v\nФорм завершенно %v\nФорм с отчётами %v\nАдрес JSON выгрузки: %s", 2, 2, 0, 0, 0, 0, ""))
	})

	t.Run("Fill self review form", func(t *testing.T) {
		secondUser := client.User("second")
		assert.Len(t, secondUser.Messages(), 1)
		msg := secondUser.Messages().Last()

		msg.ClickByText("Заполнить форму", true)

		secondUser.SelectUsersByText("Выберите ревьюеров", []string{"first", "third"})
		secondUser.Type("Достижение #1", "make app for perf review")
		secondUser.SelectByText("Оценка для достижения #1", "Превосходит ожидания")
		secondUser.SubmitForm()

		msg.WaitUpdate()

		msg.SearchByText("Отправить на ревью")
	})

	t.Run("Submit self review", func(t *testing.T) {
		secondUser := client.User("second")
		msg := secondUser.Messages().Last()

		msg.ClickByText("Отправить на ревью", false)
		msg.WaitUpdate()
		msg.SearchByText("Ждём аппрува")
	})

	t.Run("manager get message for approve", func(t *testing.T) {
		firstUser := client.User("first")
		assert.Len(t, firstUser.Messages(), 1)
		msg := firstUser.Messages().Last()

		msg.SearchByText("Нужен аппрув self-review для <@second>")
		msg.SearchByText("*make app for perf review*\nОценка: Превосходит ожидания (4)")
	})

	t.Run("manage approve self review", func(t *testing.T) {
		firstUser := client.User("first")
		msg := firstUser.Messages()[0]

		msg.ClickByText("Аппрув", false)
		msg.WaitUpdate()

		msg.SearchByText("Спасибо за аппрув")

		secondUser := client.User("second")
		memberMessage := secondUser.Messages().Last()

		memberMessage.SearchByText("Ура, руководитель согласовал!")
	})

	t.Run("peers receive forms", func(t *testing.T) {
		firstMessage := client.User("first").Messages().Last()
		thirdMessage := client.User("third").Messages().Last()

		firstMessage.SearchByText("Привет! Твой коллега <@second> запросил у тебя ревью")
		thirdMessage.SearchByText("Привет! Твой коллега <@second> запросил у тебя ревью")
	})

	t.Run("peers submit form", func(t *testing.T) {
		firstUser := client.User("first")
		firstMessage := firstUser.Messages().Last()
		thirdUser := client.User("third")
		thirdMessage := thirdUser.Messages().Last()

		fillPeerForm(firstUser, firstMessage)
		fillPeerForm(thirdUser, thirdMessage)
	})

	t.Run("change admin stats", func(t *testing.T) {
		user := client.User("first")

		user.HomeOpen()

		user.SearchByText(fmt.Sprintf("Ревью *запущено*.\nВсего разослано форм: %v\nФорм на стадии заполнения %v\nФорм на стадии аппрува %v\nФорм на стадии peer review %v\nФорм завершенно %v\nФорм с отчётами %v\nАдрес JSON выгрузки: %s", 2, 1, 0, 0, 0, 1, ""))
	})

	t.Run("get report", func(t *testing.T) {
		user := client.User("first")

		user.HomeOpen()
		user.ClickByText("Получить отчёт :paperclip:", true)
		user.SelectUsersByText("Выбрать сотрудника", []string{"second"})
		user.SubmitForm()
	})
}

func fillPeerForm(user slacktest.User, msg *slacktest.MessageView) {
	msg.ClickByText("Открыть форму", true)

	user.SelectByText("Выберите оценку", "Соответствует ожиданиям")
	user.Type("Дополнительный комментарий", "comment from first")
	user.SubmitForm()

	msg.WaitUpdate()
	msg.SearchByText("Спасибо за ответ!")
}
