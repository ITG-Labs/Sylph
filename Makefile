.PHONY: build test vet lint ca-init cert revoke

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

ca-init:
	go run ./tools/sylphca init --dir ca

cert:
	go run ./tools/sylphca issue --dir ca --name $(NAME) --type $(TYPE)

revoke:
	go run ./tools/sylphca revoke --dir ca --name $(NAME)
