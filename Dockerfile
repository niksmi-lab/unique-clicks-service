# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/clicks-api ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/clicks-api /clicks-api

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/clicks-api"]
