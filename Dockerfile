FROM 1.15.3-alpine
RUN mkdir /app
ADD . /app
WORKDIR /app

RUN go mod download
RUN go build -o main .

CMD ["/app/main"]