# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-s -w -extldflags '-static'" \
  -o /out/pterobackup ./cmd/pterobackup

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/pterobackup /pterobackup

ENV PTEROBACKUP_CONFIG_DIR=/config
ENV PTEROBACKUP_BACKUP_DIR=/backups

EXPOSE 8080
ENTRYPOINT ["/pterobackup"]
