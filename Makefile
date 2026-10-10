.PHONY: build test vet lint ca-init cert revoke

PKGS := ./... ./services/core/... ./services/agent/...

build:
	go build $(PKGS)

test:
	go test $(PKGS)

vet:
	go vet $(PKGS)

lint:
	golangci-lint run

ca-init:
	go run ./tools/sylphca init --dir ca

cert:
	go run ./tools/sylphca issue --dir ca --name $(NAME) --type $(TYPE)

revoke:
	go run ./tools/sylphca revoke --dir ca --name $(NAME)

test-db-up:
	docker compose -f deploy/test/docker-compose.yml up -d --wait

test-db-down:
	docker compose -f deploy/test/docker-compose.yml down -v

test-db: test-db-up
	SYLPH_TEST_DB=postgres://sylph:sylph@localhost:55432/sylph_test go test -race -count=1 $(PKGS)
	$(MAKE) test-db-down
