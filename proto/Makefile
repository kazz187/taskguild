.PHONY: generate lint tidy clean

generate:
	buf generate
	cd .. && go mod tidy

lint:
	buf lint

tidy:
	cd .. && go mod tidy

clean:
	rm -rf gen/go gen/ts
