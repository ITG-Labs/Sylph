.PHONY: build test vet lint ca-init cert revoke proto

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
	go run ./tools/sylphca issue --dir ca --name $(NAME) --type $(TYPE) --role=$(ROLE)

revoke:
	go run ./tools/sylphca revoke --dir ca --name $(NAME)

test-db-up:
	docker compose -f deploy/test/docker-compose.yml up -d --wait

test-db-down:
	docker compose -f deploy/test/docker-compose.yml down -v

test-db: test-db-up
	SYLPH_TEST_DB=postgres://sylph:sylph@localhost:55432/sylph_test go test -race -count=1 $(PKGS)
	$(MAKE) test-db-down

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
