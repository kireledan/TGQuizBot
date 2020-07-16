package bot

import (
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kr/pretty"
)

type TelegramQuizBot struct {
	bot              *tgbotapi.BotAPI
	connectedClients map[int64]BotClient
}

type BotClient struct {
	chatID             int64
	QuestionInterval   time.Duration
	TimeOfNextQuestion time.Time
	Quiz               Quiz
	PersonalChannel    chan tgbotapi.Update
}

func (bc BotClient) SendMSG(api *tgbotapi.BotAPI, msg string) {
	api.Send(tgbotapi.NewMessage(bc.chatID, msg))
}

var VariousChannels map[string]chan tgbotapi.Update = map[string]chan tgbotapi.Update{}

const settingSetIntervalOneHour = "SETTING_SET_INTERVAL_ONE_HOUR"
const settingSetIntervalThreeHour = "SETTING_SET_INTERVAL_THREE_HOUR"
const settingSetIntervalFiveHour = "SETTING_SET_INTERVAL_FIVE_HOUR"

func (tg TelegramQuizBot) SetQuizInterval(chatID int64, duration time.Duration) {
	if client, ok := tg.connectedClients[chatID]; ok {
		client.QuestionInterval = duration
		client.TimeOfNextQuestion = time.Now().Add(duration)
		tg.connectedClients[chatID] = client
	}
}

func InitTelegramQuizBot(token string) TelegramQuizBot {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	teleBot := TelegramQuizBot{bot: bot, connectedClients: map[int64]BotClient{}}

	return teleBot
}

func (tg TelegramQuizBot) Setup(chatID int64) error {
	// Ask How often they want questions asked
	setupMSG := tgbotapi.NewMessage(chatID, "Hello! How often would you like a new question?")

	var numericKeyboard = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1 Hour", settingSetIntervalOneHour),
			tgbotapi.NewInlineKeyboardButtonData("3 Hours", settingSetIntervalThreeHour),
			tgbotapi.NewInlineKeyboardButtonData("5 Hours", settingSetIntervalFiveHour),
		),
	)

	setupMSG.ReplyMarkup = numericKeyboard

	tg.connectedClients[chatID] = BotClient{chatID: chatID, Quiz: GetQuiz(), PersonalChannel: make(chan tgbotapi.Update)}

	tg.bot.Send(setupMSG)

	tg.connectedClients[chatID].SendMSG(tg.bot, "Let's start off with a single question :)")
	go tg.QuizUser(tg.connectedClients[chatID])
	tg.connectedClients[chatID].SendMSG(tg.bot, "You'll receive your next question in the time span you selected!")

	return nil
}

func inTimeSpan(start, end, check time.Time) bool {
	central, err := time.LoadLocation("US/Central")
	if err != nil {
		panic(err)
	}
	if start.In(central).Before(end) {
		return !check.In(central).Before(start) && !check.In(central).After(end)
	}
	if start.In(central).Equal(end) {
		return check.Equal(start)
	}
	return !start.In(central).After(check) || !end.In(central).Before(check)
}

func (tg TelegramQuizBot) QuizUser(client BotClient) {
	question := client.Quiz.GetRandomQuestion()

	pretty.Print(question)
	if len(question.Correct) == 1 {
		tg.askSingleAnswerQuestion(client, question)
	} else if len(question.Correct) > 1 {
		tg.askMultiAnswerQuestion(client, question)
	}
}

func (tg TelegramQuizBot) createSendingChannel() {

	morning, _ := time.Parse("15:04", "07:00")
	evening, _ := time.Parse("15:04", "20:00")

	ticker := time.NewTicker(time.Minute * 5)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				// Check who needs to get a quiz...
				for _, rclient := range tg.connectedClients {
					client := rclient
					fmt.Println(client.TimeOfNextQuestion)
					if time.Now().After(client.TimeOfNextQuestion) && inTimeSpan(morning, evening, time.Now()) {
						go tg.QuizUser(client)
						client.TimeOfNextQuestion = time.Now().Add(client.QuestionInterval)
						tg.connectedClients[client.chatID] = client
					}
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

// This is where all the actions happen
func (tg TelegramQuizBot) Run() error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	tg.createSendingChannel()

	updates := tg.bot.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			tg.processCallbackQuery(update.CallbackQuery)
		}
		if update.Message != nil {
			if update.Message.Command() == "start" {
				tg.Setup(update.Message.Chat.ID)
			}
		}
		if update.Poll != nil {
			if channel, ok := VariousChannels[update.Poll.ID]; ok {
				channel <- update
			}
		}
	}

	return nil
}

func (tg TelegramQuizBot) processMessage(update *tgbotapi.Message) error {
	return nil
}

func (tg TelegramQuizBot) processCallbackQuery(callback *tgbotapi.CallbackQuery) error {
	switch callback.Data {
	case settingSetIntervalOneHour:
		tg.SetQuizInterval(callback.Message.Chat.ID, time.Hour)
		tg.bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "Quiz interval set to 1 hour!"))
	case settingSetIntervalThreeHour:
		tg.SetQuizInterval(callback.Message.Chat.ID, time.Hour*3)
		tg.bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "Quiz interval set to 3 hours!"))
	case settingSetIntervalFiveHour:
		tg.SetQuizInterval(callback.Message.Chat.ID, time.Hour*5)
		tg.bot.Send(tgbotapi.NewMessage(callback.Message.Chat.ID, "Quiz interval set to 5 hours!"))
	}
	return nil
}

func (tg TelegramQuizBot) askSingleAnswerQuestion(client BotClient, q Question) {
	pollcfg := tgbotapi.NewPoll(client.chatID, q.Question, q.Choices...)
	pollcfg.Type = "quiz"
	pollcfg.CorrectOptionID = int64(q.Correct[0])

	tg.bot.Send(pollcfg)

}

func Equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func (tg TelegramQuizBot) askMultiAnswerQuestion(client BotClient, q Question) {
	pollcfg := tgbotapi.NewPoll(client.chatID, q.Question, q.Choices...)
	pollcfg.AllowsMultipleAnswers = true

	msg, _ := tg.bot.Send(pollcfg)

	pollUpdates := make(chan tgbotapi.Update)
	VariousChannels[msg.Poll.ID] = pollUpdates

	fmt.Println("waiting....")
	update := <-pollUpdates

	Picked := []int{}
	for optionNum, option := range update.Poll.Options {
		if option.VoterCount > 0 {
			Picked = append(Picked, optionNum)
		}
	}

	if Equal(Picked, q.Correct) {
		client.SendMSG(tg.bot, "You got it!!")
	} else {
		response := "Sorry. You got it wrong :( \n The correct answers are :\n---"
		for _, answer := range q.Correct {
			response += q.Choices[answer] + "\n"
		}
		client.SendMSG(tg.bot, response)
	}

}
