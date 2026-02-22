.PHONY: generate lint tidy clean

generate:
	buf generate
	cd gen/go && go mod tidy

lint:
	buf lint

tidy:
	cd gen/go && go mod tidy

clean:
	rm -rf gen/go/taskguild gen/go/buf gen/ts
