FROM alpine:latest  
RUN apk update \
    apk --no-cache add ca-certificates tzdata
COPY TGQuizBot /bin/TGQuizBot
COPY exam1.html /exam1.html
COPY exam2.html /exam2.html
COPY exam3.html /exam3.html
COPY exam4.html /exam4.html
COPY exam5.html /exam5.html

CMD /bin/TGQuizBot