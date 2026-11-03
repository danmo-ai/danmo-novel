APP_NAME := danmo-novel
FRONTEND_DIR := $(CURDIR)/frontend
OUT_DIR := $(CURDIR)/out
RELEASE_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -w -s -X 'github.com/danmo-ai/danmo-novel/core/version.Version=$(RELEASE_VERSION)'

export DQ_APP_NAME := $(APP_NAME)
DQ_BACKEND_PORT ?= 7804
DQ_FRONTEND_PORT ?= 5804
export DQ_BACKEND_PORT
export DQ_FRONTEND_PORT

.PHONY: help backend dev start stop frontend-install frontend-dev frontend-build \
	dev-desktop test build build-sidecar clean set-release-secrets \
	pack-prereqs pack-macos-desktop pack-linux-desktop pack-windows-desktop

help:
	@echo "Danmo Novel"
	@echo "  make dev              Backend :7804 + Vite :5804"
	@echo "  make dev-desktop      Backend + Tauri webview"
	@echo "  make test             go test ./..."
	@echo "  make build            frontend + server"
	@echo "  make pack-macos-desktop | pack-linux-desktop | pack-windows-desktop"
	@echo "  make set-release-secrets                    写入发布 secrets（证书从钥匙串导出）"

dev start:
	@chmod +x scripts/*.sh
	@./scripts/start.sh

dev-desktop:
	@chmod +x scripts/*.sh
	@./scripts/start_desktop.sh

backend:
	@chmod +x scripts/*.sh
	@go run ./cmd/danmo-novel

stop:
	@chmod +x scripts/*.sh
	@./scripts/stop.sh

frontend-install:
	cd $(FRONTEND_DIR) && npm install

frontend-dev: frontend-install
	cd $(FRONTEND_DIR) && npm run dev

frontend-build:
	cd $(FRONTEND_DIR) && npm install && npm run build

build-sidecar:
	@chmod +x scripts/*.sh
	@RELEASE_VERSION=$(RELEASE_VERSION) ./scripts/build_sidecar.sh

build: frontend-build
	mkdir -p out/server
	go build -ldflags "$(LDFLAGS)" -o out/server/danmo-novel ./cmd/danmo-novel

test:
	go test ./...

pack-prereqs:
	@test -f desktop/src-tauri/tauri.conf.json
	@test -f frontend/package.json
	@echo "pack-prereqs OK"

pack-macos-desktop: pack-prereqs frontend-build
	@chmod +x scripts/*.sh
	@RELEASE_VERSION=$(RELEASE_VERSION) ./scripts/pack_desktop_macos.sh

pack-linux-desktop: pack-prereqs frontend-build
	@chmod +x scripts/*.sh
	@RELEASE_VERSION=$(RELEASE_VERSION) ./scripts/pack_desktop_linux.sh

pack-windows-desktop: pack-prereqs frontend-build
	@chmod +x scripts/*.sh
	@RELEASE_VERSION=$(RELEASE_VERSION) ./scripts/pack_desktop_windows.sh

# 写入发布 secrets（证书从钥匙串导出，无需 p12）：make set-release-secrets
set-release-secrets:
	@chmod +x scripts/*.sh
	@./scripts/gh_set_release_secrets.sh $(if $(P12),--p12 "$(P12)")

clean:
	rm -rf $(OUT_DIR)
