FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /finn-bolig .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /finn-bolig /usr/local/bin/finn-bolig
ENV DATA_DIR=/data LISTEN_ADDR=:8080
VOLUME /data
EXPOSE 8080
CMD ["finn-bolig"]
