.PHONY: build build-server build-agent run run-direct test tidy clean

COMMIT ?= $(shell git rev-parse --short HEAD)
LDFLAGS := -X github.com/kazz187/taskguild/backend/internal/version.commit=$(COMMIT)

build: build-server build-agent

build-server:
	go build -ldflags "$(LDFLAGS)" -o bin/taskguild-server ./cmd/taskguild-server/

build-agent:
	go build -ldflags "$(LDFLAGS)" -o bin/taskguild-agent ./cmd/taskguild-agent/

run:
	TASKGUILD_API_KEY=dev go run ./cmd/taskguild-server/ sentinel

run-direct:
	TASKGUILD_API_KEY=dev go run ./cmd/taskguild-server/ run

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
