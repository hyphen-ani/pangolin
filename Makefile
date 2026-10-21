.PHONY: build test run echo validate

build:
	go build -o bin/pangolin ./cmd/pangolin

test:
	go test -race ./...

run: build
	./bin/pangolin run --config examples/pangolin.yaml --log-level debug

echo:
	go run ./hack/echo

validate: build
	./bin/pangolin validate --config examples/pangolin.yaml
