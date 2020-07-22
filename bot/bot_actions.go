package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	pgx "github.com/jackc/pgx/v4"
	"github.com/kr/pretty"
)

type TelegramQuizBot struct {
	bot              *tgbotapi.BotAPI
	connectedClients map[int64]*BotClient
	DB               *pgx.Conn
	updateGroup      sync.Mutex
}

type BotClient struct {
	chatID             int64
	QuestionInterval   time.Duration
	TimeOfNextQuestion time.Time
	Quiz               Quiz
	PersonalChannel    chan tgbotapi.Update
	QuestionsCorrect   int
	QuestionsAsked     int
	QuestionsSent      int
}

func (bc BotClient) SendMSG(api *tgbotapi.BotAPI, msg string) {
	api.Send(tgbotapi.NewMessage(bc.chatID, msg))
}

var VariousChannels map[string]chan tgbotapi.Update = map[string]chan tgbotapi.Update{}

const settingSetIntervalOneHour = "SETTING_SET_INTERVAL_ONE_HOUR"
const settingSetIntervalThreeHour = "SETTING_SET_INTERVAL_THREE_HOUR"
const settingSetIntervalFiveHour = "SETTING_SET_INTERVAL_FIVE_HOUR"
const cmdSendQuestion = "SEND_QUESTION"

func (tg TelegramQuizBot) SetQuizInterval(chatID int64, duration time.Duration) {
	if client, ok := tg.connectedClients[chatID]; ok {
		client.QuestionInterval = duration
		client.TimeOfNextQuestion = time.Now().Add(duration)
		_, err := tg.DB.Exec(context.Background(), "UPDATE connected_users SET questioninterval = $1, nextquestiontime = $2 WHERE chatid = $3;", client.QuestionInterval, client.TimeOfNextQuestion, chatID)
		checkErr(err)
		tg.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Quiz interval set to %s hours!", duration.String())))
		tg.updateGroup.Lock()
		tg.connectedClients[chatID] = client
		tg.updateGroup.Unlock()
	}
}

func checkErr(err error) {
	if err != nil {
		fmt.Println("An error has occured :(")
		fmt.Println(err)
	}
}

func InitTelegramQuizBot(token string) TelegramQuizBot {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	clients := map[int64]*BotClient{}

	// Populate connected clients from
	rows, err := conn.Query(context.Background(), "SELECT * FROM connected_users")
	for rows.Next() {
		var chatid int64
		var nextquestiontime time.Time
		var questionInterval time.Duration
		var questionsAsked int
		var questionsCorrect int
		var questionsSent int
		err = rows.Scan(&chatid, &nextquestiontime, &questionInterval, &questionsAsked, &questionsCorrect, &questionsSent)
		checkErr(err)

		client := BotClient{
			chatID:             chatid,
			TimeOfNextQuestion: nextquestiontime,
			QuestionInterval:   questionInterval,
			QuestionsCorrect:   questionsCorrect,
			QuestionsAsked:     questionsAsked,
			QuestionsSent:      questionsSent,
			Quiz:               GetQuiz(),
		}
		clients[chatid] = &client
	}

	teleBot := TelegramQuizBot{bot: bot, connectedClients: clients, DB: conn}

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
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Send me a question!", cmdSendQuestion),
		),
	)

	setupMSG.ReplyMarkup = numericKeyboard

	if _, ok := tg.connectedClients[chatID]; !ok {
		tg.updateGroup.Lock()
		newClient := BotClient{chatID: chatID, Quiz: GetQuiz(), PersonalChannel: make(chan tgbotapi.Update)}
		tg.connectedClients[chatID] = &newClient
		tg.updateGroup.Unlock()
		nextTime := time.Now().Add(time.Hour)
		_, err := tg.DB.Exec(context.Background(), "INSERT INTO connected_users(chatid,nextquestiontime,questionInterval) VALUES($1,$2,$3);", chatID, nextTime, time.Hour)
		checkErr(err)
		tg.connectedClients[chatID].SendMSG(tg.bot, "Let's start off with a single question :)")
		go tg.QuizUser(tg.connectedClients[chatID])
		tg.connectedClients[chatID].SendMSG(tg.bot, "You'll receive your next question in the time span you selected!")

	}

	tg.bot.Send(setupMSG)

	return nil
}

func (tg TelegramQuizBot) SyncUserData(client *BotClient) error {
	_, err := tg.DB.Exec(context.Background(), "UPDATE connected_users SET nextquestiontime = $1, questionsAsked = $2, questionsCorrect = $3 WHERE chatid = $4;", client.TimeOfNextQuestion, client.QuestionsAsked, client.QuestionsCorrect, client.chatID)
	if err != nil {
		return err
	}
	return nil
}

func (tg TelegramQuizBot) SyncAllUserData() error {
	for _, client := range tg.connectedClients {
		err := tg.SyncUserData(client)
		if err != nil {
			fmt.Println("Unable to sync all user data :(")
			return err
		}
	}
	return nil
}

func inTimeSpan(start, end, check time.Time) bool {
	if start.Before(end) {
		return !check.Before(start) && !check.After(end)
	}
	if start.Equal(end) {
		return check.Equal(start)
	}
	return !start.After(check) || !end.Before(check)
}

func (tg TelegramQuizBot) QuizUser(client *BotClient) {
	question := client.Quiz.GetRandomQuestion()

	tg.updateGroup.Lock()
	client.QuestionsSent++
	tg.connectedClients[client.chatID] = client
	tg.updateGroup.Unlock()

	pretty.Print(question)
	if len(question.Correct) == 1 {
		tg.askSingleAnswerQuestion(client, question)
	} else if len(question.Correct) > 1 {
		tg.askMultiAnswerQuestion(client, question)
	}
}

func (tg TelegramQuizBot) createSendingChannel() {

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
					if time.Now().After(client.TimeOfNextQuestion) {
						fmt.Println("TIME TO SEND A MESSAGE TO ", client.chatID)
						go tg.QuizUser(client)
						fmt.Println(client.TimeOfNextQuestion)
						fmt.Println("UPDATING THE TIME ")
						fmt.Println(client.QuestionInterval)
						client.TimeOfNextQuestion = time.Now().Add(client.QuestionInterval)
						fmt.Println(client.TimeOfNextQuestion)
						fmt.Println(time.Now().Add(client.QuestionInterval))
						tg.updateGroup.Lock()
						tg.connectedClients[client.chatID] = client
						tg.updateGroup.Unlock()
						tg.SyncUserData(client)
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
			if update.Message.Command() == "quiz" {
				go tg.QuizUser(tg.connectedClients[update.Message.Chat.ID])
			}
			if update.Message.Command() == "stats" {
				tg.SendUserStatistics(tg.connectedClients[update.Message.Chat.ID])
			}
			if update.Message.Command() == "next" {
				tg.connectedClients[update.Message.Chat.ID].SendMSG(tg.bot, tg.connectedClients[update.Message.Chat.ID].TimeOfNextQuestion.String())
			}
		}
		if update.Poll != nil {
			if channel, ok := VariousChannels[update.Poll.ID]; ok {
				fmt.Println("I got a poll response!!! from", update.Poll.ID)
				channel <- update
			}
		}
	}

	return nil
}

func (tg TelegramQuizBot) SendUserStatistics(client *BotClient) {
	statQuestion := "You have answered %d questions. With an accuracy of %d/%d (%d%%) \n I have sent you %d questions."
	if client.QuestionsAsked > 0 {
		percent := (float32(client.QuestionsCorrect) / float32(client.QuestionsAsked)) * 100
		client.SendMSG(tg.bot, fmt.Sprintf(statQuestion, client.QuestionsAsked, client.QuestionsCorrect, client.QuestionsAsked, int(percent), client.QuestionsSent))
	} else {
		client.SendMSG(tg.bot, "You haven't answered any questions yet 🥺🥺🥺🥺")
	}
}

func (tg TelegramQuizBot) processMessage(update *tgbotapi.Message) error {
	return nil
}

func (tg TelegramQuizBot) processCallbackQuery(callback *tgbotapi.CallbackQuery) error {
	switch callback.Data {
	case settingSetIntervalOneHour:
		tg.SetQuizInterval(callback.Message.Chat.ID, time.Hour)
	case settingSetIntervalThreeHour:
		tg.SetQuizInterval(callback.Message.Chat.ID, time.Hour*3)
	case settingSetIntervalFiveHour:
		tg.SetQuizInterval(callback.Message.Chat.ID, time.Hour*5)
	case cmdSendQuestion:
		go tg.QuizUser(tg.connectedClients[callback.Message.Chat.ID])

	}
	return nil
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func chunkSplit(body string, limit int) []string {
	chunks := []string{}

	for i := 0; i < len(body); i += limit {
		batch := body[i:min(i+limit, len(body))]
		chunks = append(chunks, batch)
	}

	return chunks
}

func (tg TelegramQuizBot) askSingleAnswerQuestion(client *BotClient, q Question) {
	trailingSnippet := q.Question

	if len(q.Question) >= 255 {
		questionChunks := chunkSplit(q.Question, 125)
		trailingSnippet = questionChunks[len(questionChunks)-1]
		for _, questionSnippet := range questionChunks[:len(questionChunks)-1] {
			client.SendMSG(tg.bot, questionSnippet)
		}
	}

	pollcfg := tgbotapi.NewPoll(client.chatID, trailingSnippet, q.Choices...)
	pollcfg.Type = "quiz"
	pollcfg.CorrectOptionID = int64(q.Correct[0])

	msg, _ := tg.bot.Send(pollcfg)

	fmt.Println("waiting for poll ID....", msg.Poll.ID)
	pollUpdates := make(chan tgbotapi.Update)
	VariousChannels[msg.Poll.ID] = pollUpdates
	update := <-pollUpdates

	Picked := []int{}
	for optionNum, option := range update.Poll.Options {
		if option.VoterCount > 0 {
			Picked = append(Picked, optionNum)
		}
	}

	tg.processChoice(client, q, Picked)
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

func (tg TelegramQuizBot) askMultiAnswerQuestion(client *BotClient, q Question) {
	trailingSnippet := q.Question

	if len(q.Question) >= 255 {
		questionChunks := chunkSplit(q.Question, 125)
		trailingSnippet = questionChunks[len(questionChunks)-1]
		for _, questionSnippet := range questionChunks[:len(questionChunks)-1] {
			client.SendMSG(tg.bot, questionSnippet)
		}
	}

	pollcfg := tgbotapi.NewPoll(client.chatID, trailingSnippet, q.Choices...)
	pollcfg.AllowsMultipleAnswers = true

	msg, _ := tg.bot.Send(pollcfg)

	pollUpdates := make(chan tgbotapi.Update)
	VariousChannels[msg.Poll.ID] = pollUpdates

	fmt.Println("waiting for poll ID....", msg.Poll.ID)
	update := <-pollUpdates

	Picked := []int{}
	for optionNum, option := range update.Poll.Options {
		if option.VoterCount > 0 {
			Picked = append(Picked, optionNum)
		}
	}

	tg.processChoice(client, q, Picked)
}

func (tg TelegramQuizBot) processChoice(client *BotClient, q Question, pickedAnswers []int) error {
	isCorrect := false
	if Equal(pickedAnswers, q.Correct) {
		if len(pickedAnswers) > 1 {
			client.SendMSG(tg.bot, "You got it!! ✔️")

		}
		isCorrect = true
	} else {
		response := "Sorry. You got it wrong ❌❌❌ :( \n The correct answers are :\n---"
		for _, answer := range q.Correct {
			response += q.Choices[answer] + "\n"
		}
		if len(pickedAnswers) > 1 {
			client.SendMSG(tg.bot, response)
		}
	}

	err := tg.IncrementQuestionStats(client, isCorrect)
	if err != nil {
		return err
	}

	return nil
}

func (tg TelegramQuizBot) IncrementQuestionStats(client *BotClient, isCorrect bool) error {
	tg.updateGroup.Lock()
	client.QuestionsAsked++
	if isCorrect {
		client.QuestionsCorrect++
	}
	tg.connectedClients[client.chatID] = client
	tg.SyncUserData(client)
	tg.updateGroup.Unlock()
	return nil
}
