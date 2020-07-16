package main

import (
	"os"
	"fmt"
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

func main() {
	InitializeBot()
}
