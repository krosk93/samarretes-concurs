FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod ./
COPY main.go castellers.json ./
RUN CGO_ENABLED=0 go build -o mock .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /app/mock ./mock
COPY --from=build /app/castellers.json ./castellers.json
EXPOSE 8081
CMD ["./mock"]
