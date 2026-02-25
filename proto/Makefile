.PHONY: generate lint tidy clean

generate:
	buf generate
	cd ../backend && go mod tidy

lint:
	buf lint

tidy:
	cd ../backend && go mod tidy

clean:
	rm -rf ../backend/gen/proto/taskguild ../backend/gen/proto/buf gen/ts
