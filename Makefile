.PHONY: build test run echo validate

build:
	go build -o bin/pangolin ./cmd/pangolin

test:
	go test -race ./...

run: build
	./bin/pangolin run --config examples/pangolin.yaml --log-level debug

echo:
	go run ./hack/echo --listen :9000 --name echo-a

echo2:
	go run ./hack/echo --listen :9001 --name echo-b

validate: build
	./bin/pangolin validate --config examples/pangolin.yaml

