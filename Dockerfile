FROM alpine:latest  
RUN apk update \
    apk --no-cache add ca-certificates
COPY TGQuizBot /TGQuizBot
COPY exam1.html /exam1.html

CMD ./TGQuizBot