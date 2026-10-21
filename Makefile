# Local, gitignored overrides (e.g. VOX_LLM_API_KEY) for make dev / chat
-include .env
export

# Host compiles, containers test.
ARCHES   ?= amd64 arm64
IMAGE    ?= vox-mi-agent
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
# Local-only: extra CA for TLS-intercepting proxies (certs/ is gitignored, CI has none)
comma    := ,
EXTRA_CA ?= $(wildcard certs/extra-ca.crt)
SECRET   := $(if $(EXTRA_CA),--secret id=extraca$(comma)src=$(EXTRA_CA))
export VOX_TOOLS_DIR ?= $(CURDIR)/.tools

.PHONY: tools build web test test-docker image dev clean

tools: .tools/fzq
.tools/fzq: tools.lock
	./scripts/fetch-tools.sh .tools

build: web
	@for a in $(ARCHES); do \
	  echo "build linux/$$a"; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -trimpath -ldflags '$(LDFLAGS)' -o dist/linux_$$a/app ./cmd/app || exit 1; \
	done

web:
	@if [ -f web/package.json ]; then cd web && npm ci --silent && npm run build; fi
	@if [ -d web/dist ]; then rm -rf internal/webui/dist && mkdir -p internal/webui/dist && cp -r web/dist/. internal/webui/dist/; fi

test: tools
	go test ./...

test-docker:
	docker buildx build --load $(SECRET) --target test -t $(IMAGE):test .
	docker compose -f docker-compose.test.yaml run --rm test; rc=$$?; \
	docker rmi -f $(IMAGE):test >/dev/null 2>&1; exit $$rc

image: build
	docker buildx build --platform $(shell echo $(ARCHES) | sed 's/[^ ]*/linux\/&/g; s/ /,/g') $(SECRET) --target runtime -t $(IMAGE):$(VERSION) .

image-local: build
	docker buildx build --load $(SECRET) --target runtime -t $(IMAGE):dev .

dev: tools
	go run ./cmd/app serve --listen tcp://127.0.0.1:8080 --base-path "" --data ./data

clean:
	rm -rf dist web/dist

# Run the runtime image locally with .env (music mount, LLM key). Data in $$VOX_DATA.
up: image-local
	docker compose --env-file .env -f app/docker/docker-compose.yaml up -d
down:
	docker compose --env-file .env -f app/docker/docker-compose.yaml down
logs:
	docker compose --env-file .env -f app/docker/docker-compose.yaml logs -f --tail=100
