SHELL := /bin/bash
export PATH := $(HOME)/.local/go/bin:$(HOME)/.local/bin:$(HOME)/go/bin:$(PATH)
export GOPATH := $(HOME)/go
export LD_LIBRARY_PATH := $(HOME)/.local/lib:$(LD_LIBRARY_PATH)

GO := go
GODOT := godot4
PROTOC := protoc
GOOSE := goose
GOODIE := $(GOODIE)

# Env file: real secrets live at ~/aetheria/env (never in repo).
ENV_FILE := $(HOME)/aetheria/env
-include $(ENV_FILE)
export

PG_URL := postgres://$(AETHERIA_PG_USER):$(AETHERIA_PG_PASSWORD)@$(AETHERIA_PG_HOST):$(AETHERIA_PG_PORT)/$(AETHERIA_PG_DB)?sslmode=disable

.PHONY: all build test vet fmtcheck content migrate bottest loadtest questrun deploy export-client client-tests backup clean

all: test

# ---- codegen ----
.PHONY: content
content:
	protoc -I shared/proto --go_out=paths=source_relative:server/gen shared/proto/*.proto
	python3 tools/protogen/protogen.py
	gofmt -l server/gen >/dev/null 2>&1 || true
	@echo "protocol codegen done (Go + Godot)"

# ---- build ----
build:
	@mkdir -p server/bin
	go build -o server/bin/authserver ./server/cmd/authserver
	go build -o server/bin/gameserver ./server/cmd/gameserver
	go build -o server/bin/adminserver ./server/cmd/adminserver
	go build -o server/bin/portal ./server/cmd/portal
	@echo "built: authserver gameserver adminserver portal"

# ---- static checks ----
vet:
	go vet ./...

fmtcheck:
	@bad=$$(gofmt -l $$(find server tools -name '*.go' -not -path '*/gen/*')); \
	if [ -n "$$bad" ]; then echo "gofmt needed: $$bad"; exit 1; fi

# ---- unit tests ----
test: vet fmtcheck content
	go test ./...
	@$(MAKE) --no-print-directory client-tests

client-tests:
	@cd client && $(GODOT) --headless --path . --script res://scripts/test_proto.gd 2>&1 | grep -q "ALL PASS" && echo "godot proto tests: PASS"

# ---- database ----
migrate:
	$(GOOSE) -dir deploy/migrations postgres "$(PG_URL)" up

migrate-status:
	$(GOOSE) -dir deploy/migrations postgres "$(PG_URL)" status

# ---- bot / integration tests ----
bottest:
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -profile ping
	@echo "bottest: ping profile OK"

loadtest:
	@echo "loadtest (M2+): spawn $(N) botclients for $(DURATION)"

questrun:
	@echo "questrun (M5+): master regression playthrough"

# ---- client export ----
export-client:
	@cd client && $(GODOT) --headless --export-release Linux . 2>&1 | tail -3 && \
	$(GODOT) --headless --export-release Windows . 2>&1 | tail -3
	@echo "client exported to client/build/"

# ---- deploy ----
deploy: build
	./deploy/deploy.sh
	@echo "deploy done"

backup:
	./deploy/backup.sh

clean:
	rm -rf server/bin
