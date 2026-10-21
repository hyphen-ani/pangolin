.PHONY: build test run echo

build:
	go build -o bin/pangolin ./cmd/pangolin

test:
	go test -race ./...

run: build
	./bin/pangolin --upstream http://127.0.0.1:9000/api --log-level debug


echo:
	go run ./hack/echo