FROM golang:1.26.10

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

RUN go install github.com/air-verse/air@latest

ENV PATH="/go/bin:${PATH}"

COPY . .

EXPOSE 8080

CMD ["air"]