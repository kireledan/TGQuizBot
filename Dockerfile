FROM alpine:latest  
RUN apk update \
    apk --no-cache add ca-certificates tzdata
COPY TGQuizBot /bin/TGQuizBot

CMD /bin/TGQuizBot