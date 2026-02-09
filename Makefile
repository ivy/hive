BINARY  := hive
PREFIX  := $(HOME)/.local/bin

.PHONY: build install

build:
	go build -o $(BINARY) ./cmd/hive/

install: build
	install -d $(PREFIX)
	install -m 755 $(BINARY) $(PREFIX)/$(BINARY)
