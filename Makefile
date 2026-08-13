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
	@cd client && $(GODOT) --headless --path . --import >/dev/null 2>&1 || true
	@cd client && $(GODOT) --headless --path . --script res://scripts/test_proto.gd 2>&1 | grep -q "ALL PASS" && echo "godot proto tests: PASS"
	@cd client && $(GODOT) --headless --path . --script res://scripts/test_session.gd 2>&1 | grep -q "ALL PASS" && echo "godot session tests: PASS"
	@cd client && $(GODOT) --headless --path . --script res://scripts/test_scenes.gd 2>&1 | grep -q "ALL PASS" && echo "godot scene tests: PASS"

# ---- database ----
migrate:
	$(GOOSE) -dir deploy/migrations postgres "$(PG_URL)" up

migrate-status:
	$(GOOSE) -dir deploy/migrations postgres "$(PG_URL)" status

# ---- bot / integration tests ----
bottest:
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile full-auth
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile presence -duration 10s
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile roamer -n 5 -duration 12s
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile chaos -duration 5s
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile chat
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile combat
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile trader
	@echo "bottest: full-auth + presence + roamer + chaos + chat + combat + trader OK (M1 + M2 + M3 + M4 acceptance)"

loadtest:
	@echo "loadtest (M2+): spawn $(N) botclients for $(DURATION)"

combat-soak: ## M3 acceptance soak: N bots combat for DURATION (asserts tick p99 < 50ms)
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -ctrl http://127.0.0.1:$(AETHERIA_CONTROL_PORT) -profile combat-soak -n $(N) -duration $(DURATION)

questrun:
	go run ./tools/botclient -addr ws://127.0.0.1:$(AETHERIA_GAME_PORT)/ws -api http://127.0.0.1:$(AETHERIA_AUTH_PORT) -profile quester
	@echo "questrun: full Havenport quest chain complete (M5 acceptance)"

# ---- client export ----
export-client:
	@mkdir -p client/build
	@cd client && $(GODOT) --headless --export-release Linux build/aetheria-linux.x86_64 2>&1 | grep -q "ERROR" && exit 1 || true
	@cd client && $(GODOT) --headless --export-release Windows build/aetheria-windows.exe 2>&1 | grep -q "ERROR" && exit 1 || true
	@cp client/config.json client/build/config.json
	@echo "client exported to client/build/ (linux + windows + config.json)"

# ---- deploy ----
deploy: build
	./deploy/deploy.sh
	@echo "deploy done"

backup:
	./deploy/backup.sh

clean:
	rm -rf server/bin
