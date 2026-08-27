VERSION ?= dev

.PHONY: run test test-race vet build up down prometheus-check

run:
	go run ./cmd

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -trimpath -ldflags="-X main.version=$(VERSION)" -o bin/clicks-api ./cmd

up:
	docker compose up --build -d

down:
	docker compose down

prometheus-check:
	promtool check config deployments/prometheus/prometheus.yml
	promtool check rules deployments/prometheus/alerts.yml
