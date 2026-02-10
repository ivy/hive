BINARY          := hive
PREFIX          := $(HOME)/.local/bin
SYSTEMD_USER_DIR := $(HOME)/.config/systemd/user
VERSION         ?= dev
BUILD_TIME      := $(shell date -u '+%Y%m%d.%H%M%S')
LDFLAGS         := -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

.PHONY: build install install-units uninstall-units

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/hive/

install: build
	install -d $(PREFIX)
	install -m 755 $(BINARY) $(PREFIX)/$(BINARY)

install-units: ## Install systemd user units
	mkdir -p $(SYSTEMD_USER_DIR)
	cp contrib/systemd/*.service contrib/systemd/*.target contrib/systemd/*.timer $(SYSTEMD_USER_DIR)/
	systemctl --user daemon-reload

uninstall-units: ## Remove systemd user units
	rm -f $(SYSTEMD_USER_DIR)/hive@.target
	rm -f $(SYSTEMD_USER_DIR)/hive-poll@.service
	rm -f $(SYSTEMD_USER_DIR)/hive-run@.service
	rm -f $(SYSTEMD_USER_DIR)/hive-reap@.service
	rm -f $(SYSTEMD_USER_DIR)/hive-reap@.timer
	systemctl --user daemon-reload
