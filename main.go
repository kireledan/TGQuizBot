package main

import (
	"fmt"
	"os"

	"github.com/jmoiron/sqlx"
	"github.com/kireledan/TGQuizBot/bot"
)

func InitializeBot() {
	token, hastoken := os.LookupEnv("TELEGRAM_TOKEN")
	if !hastoken {
		fmt.Println("NO TOKEN PROVIDED! PLEASE POPULATE $TELEGRAM_TOKEN")
		os.Exit(1)
	}
	bot := bot.InitTelegramQuizBot(token)
	bot.Run()

}

var QuizMaps map[string][]string = map[string][]string{
	"network+":  []string{"./exam1.html", "./exam2.html", "./exam3.html", "./exam4.html", "./exam5.html"},
	"security+": []string{"./secplus/exam1.html", "./secplus/exam2.html", "./secplus/exam3.html", "./secplus/exam4.html"},
}

func Tester() {
	conn, err := sqlx.Open("pgx", "REDACTED")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	quiz := bot.BuildQuizFromDB("security+", conn)
	for _, ques := range quiz.Questions {
		fmt.Println(ques)
	}
	user := bot.BotClient{}
	err = conn.Get(&user, "SELECT * FROM connected_users WHERE chatid = $1", 32150951)
	if err != nil {
		fmt.Println(err)
	}
}

func PrintQuizzes() {
	qu := bot.GetQuiz(QuizMaps["security+"], "security+")
	bot.UploadQuizToDB(qu)
	qu = bot.GetQuiz(QuizMaps["network+"], "network+")
	bot.UploadQuizToDB(qu)
}

func main() {
	if len(os.Args) > 1 {
		Tester()
	} else {
		InitializeBot()
	}
}
