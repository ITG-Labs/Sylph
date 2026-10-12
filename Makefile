.PHONY: build test vet lint ca-init cert revoke proto test-db-up test-db-down test-db test-xray-up test-xray-down test-xray test-node test-subs

PKGS := ./... ./services/core/... ./services/agent/... ./services/subs/...

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
	go run ./tools/sylphca issue --dir ca --name $(NAME) --type $(TYPE) --role=$(ROLE)

revoke:
	go run ./tools/sylphca revoke --dir ca --name $(NAME)

test-db-up:
	docker compose -f deploy/test/docker-compose.yml up -d --wait

test-db-down:
	docker compose -f deploy/test/docker-compose.yml down -v

test-db: test-db-up
	SYLPH_TEST_DB=postgres://sylph:sylph@localhost:55432/sylph_test go test -race -count=1 $(PKGS); \
	status=$$?; $(MAKE) test-db-down; exit $$status

test-xray-up:
	docker compose -f deploy/test/xray/docker-compose.yml up -d --wait

test-xray-down:
	docker compose -f deploy/test/xray/docker-compose.yml down

test-xray: test-xray-up
	SYLPH_TEST_XRAY=127.0.0.1:55085 go test -race -count=1 ./services/agent/...; \
	status=$$?; $(MAKE) test-xray-down; exit $$status

NODE_COMPOSE := docker compose -f deploy/test/node/docker-compose.yml

test-node:
	mkdir -p deploy/test/node/bin/xray
	CGO_ENABLED=0 go build -o deploy/test/node/bin/sylph-agent ./services/agent/cmd/agent
	CGO_ENABLED=0 go test -c -o deploy/test/node/bin/agent.test ./services/agent
	$(NODE_COMPOSE) build
	$(NODE_COMPOSE) run --rm agent; \
	status=$$?; $(NODE_COMPOSE) --profile test down -v; exit $$status

SUBS_COMPOSE := docker compose -f deploy/test/subs/docker-compose.yml
SUBS_BIN := deploy/test/subs/bin
SUBS_CA := go run ./tools/sylphca issue --dir $(SUBS_BIN)/ca

test-subs:
	mkdir -p deploy/test/node/bin/xray $(SUBS_BIN)
	CGO_ENABLED=0 go build -o deploy/test/node/bin/sylph-agent ./services/agent/cmd/agent
	CGO_ENABLED=0 go test -c -o deploy/test/node/bin/agent.test ./services/agent
	CGO_ENABLED=0 go build -o $(SUBS_BIN)/sylph-core ./services/core/cmd/core
	CGO_ENABLED=0 go build -o $(SUBS_BIN)/sylph-subs ./services/subs/cmd/subs
	CGO_ENABLED=0 go test -c -o $(SUBS_BIN)/subs.test ./services/subs
	rm -rf $(SUBS_BIN)/ca
	go run ./tools/sylphca init --dir $(SUBS_BIN)/ca
	$(SUBS_CA) --name core.sylph.internal --type server
	$(SUBS_CA) --name panel --type client --role panel
	$(SUBS_CA) --name subs --type client --role subs
	$(SUBS_CA) --name node-1 --type client --role node
	chmod -R a+rX $(SUBS_BIN)/ca
	$(SUBS_COMPOSE) build
	$(SUBS_COMPOSE) run --rm runner; \
	status=$$?; $(SUBS_COMPOSE) --profile test down -v; exit $$status

PROTOC_GEN_GO := v1.36.12
PROTOC_GEN_GO_GRPC := v1.6.2
AGENT_MODULE := github.com/ITG-Labs/sylph/services/agent
XRAY_PROTO := services/agent/xrayproto
XRAY_PROTO_MAP := \
	app/proxyman/command/command.proto=$(AGENT_MODULE)/internal/xrayapi/proxymancmd;proxymancmd \
	app/stats/command/command.proto=$(AGENT_MODULE)/internal/xrayapi/statscmd;statscmd \
	common/protocol/user.proto=$(AGENT_MODULE)/internal/xrayapi/protocol;protocol \
	common/serial/typed_message.proto=$(AGENT_MODULE)/internal/xrayapi/serial;serial \
	core/config.proto=$(AGENT_MODULE)/internal/xrayapi/core;core \
	proxy/vless/account.proto=$(AGENT_MODULE)/internal/xrayapi/vless;vless

proto:
	GOBIN=$(CURDIR)/.bin go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO)
	GOBIN=$(CURDIR)/.bin go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC)
	rm -rf services/agent/internal/xrayapi
	protoc -I $(XRAY_PROTO) \
		--plugin=protoc-gen-go=$(CURDIR)/.bin/protoc-gen-go \
		--plugin=protoc-gen-go-grpc=$(CURDIR)/.bin/protoc-gen-go-grpc \
		--go_out=services/agent --go_opt=module=$(AGENT_MODULE) \
		--go-grpc_out=services/agent --go-grpc_opt=module=$(AGENT_MODULE) \
		$(foreach m,$(XRAY_PROTO_MAP),'--go_opt=M$(m)' '--go-grpc_opt=M$(m)') \
		$(foreach m,$(XRAY_PROTO_MAP),$(firstword $(subst =, ,$(m))))
