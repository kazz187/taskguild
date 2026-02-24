.PHONY: build build-server build-agent run test tidy clean

build: build-server build-agent

build-server:
	go build -o bin/taskguild-server ./cmd/taskguild-server/

build-agent:
	go build -o bin/taskguild-agent ./cmd/taskguild-agent/

run:
	TASKGUILD_API_KEY=dev go run ./cmd/taskguild-server/

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
