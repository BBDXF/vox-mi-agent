# Local, gitignored overrides (e.g. VOX_LLM_API_KEY) for make dev / chat
-include .env
export

# Host compiles, containers test.
ARCHES   ?= amd64 arm64
IMAGE    ?= vox-mi-agent
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BI       := github.com/BBDXF/vox-mi-agent/internal/buildinfo
LDFLAGS  := -s -w -X $(BI).Version=$(VERSION) -X $(BI).Commit=$(COMMIT) -X $(BI).Date=$(DATE)
export VOX_TOOLS_DIR ?= $(CURDIR)/.tools

.PHONY: tools build web test test-docker image dev clean fpk

tools: .tools/fzq
.tools/fzq: tools.lock
	./scripts/fetch-tools.sh .tools

build: web
	@for a in $(ARCHES); do \
	  echo "build linux/$$a"; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -trimpath -ldflags '$(LDFLAGS)' -o dist/linux_$$a/app ./cmd/app || exit 1; \
	done

web:
	@if [ -f web/package.json ]; then cd web && \
	  if [ ! -d node_modules ] || [ package-lock.json -nt node_modules ]; then \
	    $(if $(wildcard certs/extra-ca.crt),[ -s ../certs/extra-ca.crt ] && export NODE_EXTRA_CA_CERTS=../certs/extra-ca.crt;) npm ci --silent --no-audit --no-fund; fi && \
	  npm run build; fi
	@if [ -d web/dist ]; then rm -rf internal/webui/dist && mkdir -p internal/webui/dist && cp -r web/dist/. internal/webui/dist/ && touch internal/webui/dist/.gitkeep; fi

test: tools
	go test ./...

test-docker:
	docker buildx build --load --target test -t $(IMAGE):test .
	docker compose -f compose.test.yaml run --rm test; rc=$$?; \
	docker rmi -f $(IMAGE):test >/dev/null 2>&1; exit $$rc

image: build
	docker buildx build --platform $(shell echo $(ARCHES) | sed 's/[^ ]*/linux\/&/g; s/ /,/g') --target runtime -t $(IMAGE):$(VERSION) .

image-local: build
	docker buildx build --load --target runtime -t $(IMAGE):dev .

dev: tools
	go run ./cmd/app serve --listen tcp://127.0.0.1:8080 --base-path "" --data ./data

clean:
	rm -rf dist web/dist

# Run the runtime image locally with .env (music mount, LLM key). Data in $$VOX_DATA.
# compose.local.yaml (mounts certs/ for TLS-intercepting proxies) only if extra-ca.crt is non-empty.
COMPOSE = docker compose --env-file .env -f compose.yaml $(if $(shell test -s certs/extra-ca.crt && echo y),-f compose.local.yaml)
up: image-local
	$(COMPOSE) up -d
down:
	$(COMPOSE) down
logs:
	$(COMPOSE) logs -f --tail=100

fpk:
	sh fnos/build.sh $(VERSION)
