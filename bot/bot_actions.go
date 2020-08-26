package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/jackc/pgx/stdlib"
	pgx "github.com/jackc/pgx/v4"
	"github.com/jmoiron/sqlx"

	"github.com/go-redis/redis/v8"
	"github.com/kr/pretty"
)

type TelegramQuizBot struct {
	bot              *tgbotapi.BotAPI
	connectedClients map[int64]*BotClient
	DB               *sqlx.DB
	updateGroup      sync.Mutex
}

type BotClient struct {
	chatID             int64
	QuestionInterval   time.Duration
	TimeOfNextQuestion time.Time
	LastAnsweredTime   time.Time
	Quiz               Quiz
	QuestionsCorrect   int
	QuestionsAsked     int
	QuestionsSent      int
	QuizSection        string
	Username           string
}

func (bc BotClient) SendMSG(api *tgbotapi.BotAPI, msg string) {
	sentmsg, _ := api.Send(tgbotapi.NewMessage(bc.chatID, msg))
	bc.Username = sentmsg.Chat.UserName
}

const settingSetIntervalOneHour = "SETTING_SET_INTERVAL_ONE_HOUR"
const settingSetIntervalThreeHour = "SETTING_SET_INTERVAL_THREE_HOUR"
const settingSetIntervalFiveHour = "SETTING_SET_INTERVAL_FIVE_HOUR"
const cmdSendQuestion = "SEND_QUESTION"

func (tg TelegramQuizBot) SetQuizInterval(chatID int64, duration time.Duration) {
	if client, ok := tg.connectedClients[chatID]; ok {
		client.QuestionInterval = duration
		client.TimeOfNextQuestion = time.Now().Add(duration)
		_, err := tg.DB.ExecContext(context.Background(), "UPDATE connected_users SET questioninterval = $1, nextquestiontime = $2 WHERE chatid = $3;", client.QuestionInterval.String(), string(pq.FormatTimestamp(client.TimeOfNextQuestion)), chatID)
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

func GetPoll(pollID string) *tgbotapi.Poll {
	opts, _ := redis.ParseURL(os.Getenv("REDIS_URL"))
	opts.Username = ""
	c := redis.NewClient(opts)
	defer c.Close()
	v := c.Get(context.Background(), pollID).Val()
	fmt.Println(v)
	p := tgbotapi.Message{}
	json.Unmarshal([]byte(v), &p)
	if p.Poll != nil {
		fmt.Println("Got the poll!!!")
		return p.Poll
	}
	return nil

}

func RemovePoll(pollID string) {
	opts, _ := redis.ParseURL(os.Getenv("REDIS_URL"))
	opts.Username = ""
	c := redis.NewClient(opts)
	defer c.Close()
	v := c.Del(context.Background(), pollID).Err()
	if v != nil {
		fmt.Println(v)
	}
}

func UploadPoll(p tgbotapi.Message) {
	opts, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		panic(err)
	}
	opts.Username = ""
	c := redis.NewClient(opts)
	defer c.Close()
	bytes, err := json.Marshal(p)
	err = c.Set(context.Background(), p.Poll.ID, string(bytes), 0).Err()
	if err != nil {
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

	botConnection, err := sqlx.Open("pgx", os.Getenv("DATABASE_URL"))
	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(context.Background())

	clients := map[int64]*BotClient{}

	// Populate connected clients from
	rows, err := conn.Query(context.Background(), "SELECT * FROM connected_users")
	defer rows.Close()
	for rows.Next() {
		var chatid int64
		var nextquestiontime time.Time
		var lastansweredtime time.Time
		var questionInterval time.Duration
		var questionsAsked int
		var questionsCorrect int
		var questionsSent int
		var quizSection string
		var username string
		err = rows.Scan(&chatid, &nextquestiontime, &questionInterval, &questionsAsked, &questionsCorrect, &questionsSent, &quizSection, &lastansweredtime, &username)
		checkErr(err)

		client := BotClient{
			chatID:             chatid,
			TimeOfNextQuestion: nextquestiontime,
			QuestionInterval:   questionInterval,
			QuestionsCorrect:   questionsCorrect,
			QuestionsAsked:     questionsAsked,
			QuestionsSent:      questionsSent,
			QuizSection:        quizSection,
			LastAnsweredTime:   lastansweredtime,
			Username:           username,
			Quiz:               BuildQuizFromDB(quizSection, botConnection),
		}
		clients[chatid] = &client
	}

	teleBot := TelegramQuizBot{bot: bot, connectedClients: clients, DB: botConnection}

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
		newClient := BotClient{chatID: chatID, Quiz: BuildQuizFromDB("network+", tg.DB)}
		tg.connectedClients[chatID] = &newClient
		tg.updateGroup.Unlock()
		nextTime := time.Now().Add(time.Hour)
		_, err := tg.DB.ExecContext(context.Background(), "INSERT INTO connected_users(chatid,nextquestiontime,questionInterval,questionssent,quizsection,lastansweredtime,username) VALUES($1,$2,$3,$4,$5,$6,$7);", chatID, nextTime, time.Hour, 0, "network+", time.Now(), "unknown")
		checkErr(err)
		tg.connectedClients[chatID].SendMSG(tg.bot, "Let's start off with a single question :)")
		tg.QuizUser(tg.connectedClients[chatID])
		tg.connectedClients[chatID].SendMSG(tg.bot, "You'll receive your next question in the time span you selected!")

	}

	tg.bot.Send(setupMSG)

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

	pretty.Print(question)
	if len(question.Correct) == 1 {
		question.Question += fmt.Sprintf("[ID: %s]", question.ID)
		tg.askSingleAnswerQuestion(client, question)
	} else if len(question.Correct) > 1 {
		question.Question += fmt.Sprintf("[ID: %s]", question.ID)
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
						tg.QuizUser(client)
						fmt.Println(client.TimeOfNextQuestion)
						fmt.Println("UPDATING THE TIME ")
						fmt.Println(client.QuestionInterval)
						client.TimeOfNextQuestion = time.Now().Add(client.QuestionInterval)
						fmt.Println(client.TimeOfNextQuestion)
						fmt.Println(time.Now().Add(client.QuestionInterval))

						_, err := tg.DB.ExecContext(context.Background(), "UPDATE connected_users SET nextquestiontime = $1 WHERE chatid = $2;", string(pq.FormatTimestamp(client.TimeOfNextQuestion)), client.chatID)
						if err != nil {
							fmt.Println(err)
							fmt.Println("unable to update user time")
						}
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
				tg.QuizUser(tg.connectedClients[update.Message.Chat.ID])
			}
			if update.Message.Command() == "stats" {
				tg.SendUserStatistics(tg.connectedClients[update.Message.Chat.ID])
			}
			if update.Message.Command() == "networkquiz" {
				tg.SetQuizSection(tg.connectedClients[update.Message.Chat.ID], "network+")
			}
			if update.Message.Command() == "securityquiz" {
				tg.SetQuizSection(tg.connectedClients[update.Message.Chat.ID], "security+")
			}
			if update.Message.Command() == "next" {
				tg.connectedClients[update.Message.Chat.ID].SendMSG(tg.bot, tg.connectedClients[update.Message.Chat.ID].TimeOfNextQuestion.String())
			}
		}
		if update.PollAnswer != nil {
			p := GetPoll(update.PollAnswer.PollID)
			if p != nil {
				q := tg.GetQuestionFromPollUpdate(*p)
				picked := make([]int64, len(update.PollAnswer.OptionIDs))
				for i := range update.PollAnswer.OptionIDs {
					picked[i] = int64(update.PollAnswer.OptionIDs[i])
				}
				cid := int64(update.PollAnswer.User.ID)
				if client, ok := tg.connectedClients[cid]; ok {
					err := tg.processChoice(client, q, picked)
					if err != nil {
						fmt.Println(err)
					}
					RemovePoll(update.PollAnswer.PollID)
				}
			}
		}
	}

	return nil
}

func (tg TelegramQuizBot) GetQuestionFromPollUpdate(poll tgbotapi.Poll) Question {
	re := regexp.MustCompile(`\[ID:([^\[\]]*)\]`)
	submatchall := re.FindStringSubmatch(poll.Question)
	fmt.Println("Successfully got the poll!!!!!!!!!!!")

	return GetQuestionByID(strings.TrimSpace(submatchall[1]), tg.DB)
}

func (tg TelegramQuizBot) SetQuizSection(client *BotClient, section string) {
	client.QuizSection = section
	client.Quiz = BuildQuizFromDB(section, tg.DB)
	_, err := tg.DB.ExecContext(context.Background(), "UPDATE connected_users SET quizsection = $1 WHERE chatid = $2;", section, client.chatID)
	checkErr(err)
	client.SendMSG(tg.bot, "Quiz selection set to"+section)
}

func (tg TelegramQuizBot) SendUserStatistics(client *BotClient) {
	statQuestion := "You have answered %d questions. With an accuracy of %d/%d (%d%%) \n I have sent you %d questions."
	qclient := BotClient{}
	err := tg.DB.Get(&qclient, "SELECT questionscorrect, questionsasked, questionssent FROM connected_users WHERE chatid=$1;", client.chatID)
	if err != nil {
		fmt.Println(err)
	}
	if client.QuestionsAsked > 0 {
		percent := (float32(qclient.QuestionsCorrect) / float32(qclient.QuestionsAsked)) * 100
		client.SendMSG(tg.bot, fmt.Sprintf(statQuestion, qclient.QuestionsAsked, qclient.QuestionsCorrect, qclient.QuestionsAsked, int(percent), qclient.QuestionsSent))
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
		tg.QuizUser(tg.connectedClients[callback.Message.Chat.ID])

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
	pollcfg.IsAnonymous = false

	msg, _ := tg.bot.Send(pollcfg)
	if msg.Chat != nil {
		Username := msg.Chat.UserName
		_, err := tg.DB.ExecContext(context.Background(), "UPDATE connected_users SET username = $1 WHERE chatid = $2;", Username, client.chatID)
		checkErr(err)
	}

	if msg.Poll != nil {
		UploadPoll(msg)
	}
}

func Equal(a, b []int64) bool {
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
	pollcfg.IsAnonymous = false

	msg, _ := tg.bot.Send(pollcfg)

	if msg.Poll != nil {
		fmt.Println("UPloading multi poll....")
		UploadPoll(msg)
	}
}

func (tg TelegramQuizBot) processChoice(client *BotClient, q Question, pickedAnswers []int64) error {
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
		fmt.Println(err)
		return err
	}

	fmt.Println("Updating last answered time")
	_, err = tg.DB.ExecContext(context.Background(), "UPDATE connected_users SET lastansweredtime = $1 WHERE chatid = $2;", time.Now(), client.chatID)
	if err != nil {
		fmt.Println("Failed to update lastanswered stats")
		fmt.Println(err)
		return err
	}

	return nil
}

func (tg TelegramQuizBot) IncrementQuestionStats(client *BotClient, isCorrect bool) error {
	client.QuestionsAsked++
	if isCorrect {
		client.QuestionsCorrect++
	}
	_, err := tg.DB.ExecContext(context.Background(), "UPDATE connected_users SET questionsAsked = $1, questionsCorrect = $2 WHERE chatid = $3;", client.QuestionsAsked, client.QuestionsCorrect, client.chatID)
	if err != nil {
		fmt.Println("Failed to update question stats")
		return err
	}
	return nil
}
