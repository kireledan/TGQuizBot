CGO_ENABLED=0 GOOS=linux go build -v -o TGQuizBot
heroku container:push sagebot -a sagebot-quizzer
heroku container:release sagebot -a sagebot-quizzer