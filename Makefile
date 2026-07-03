# Snorg — Supernote Organizer (github.com/jdlugosz963/snorg)

BIN   := snorg
CMD   := ./cmd/snorg

build:
	go build -o $(BIN) $(CMD)

install: install-completions-fish
	go install $(CMD)

test:
	go test ./...

test-fast:
	go test ./internal/snote/sntool

test-e2e:
	go test -run TestIngestSampleNote ./internal/ingest

lint:
	go vet ./... && gofmt -l .

fmt:
	gofmt -w .

tidy:
	go mod tidy

ingest:
	go run $(CMD) ingest

list:
	go run $(CMD) list

retrieve:
	go run $(CMD) retrieve

query:
	go run $(CMD) query

analyze:
	go run $(CMD) analyze

export:
	go run $(CMD) export

add-go-path:
	@if ! grep -q 'fish_add_path.*go/bin' ~/.config/fish/config.fish 2>/dev/null; then \
		echo 'fish_add_path $(shell go env GOPATH)/bin' >> ~/.config/fish/config.fish; \
		echo "added ~/go/bin to fish PATH (~/.config/fish/config.fish)"; \
	fi

install-completions-fish: add-go-path
	mkdir -p ~/.config/fish/completions
	cp contrib/completions/snorg.fish ~/.config/fish/completions/

install-completions-bash:
	sudo mkdir -p /usr/local/share/bash-completion/completions
	sudo cp contrib/completions/snorg.bash /usr/local/share/bash-completion/completions/snorg

install-completions-zsh:
	sudo mkdir -p /usr/local/share/zsh/site-functions
	sudo cp contrib/completions/snorg.zsh /usr/local/share/zsh/site-functions/_snorg

install-completions: install-completions-fish

clean:
	rm -f $(BIN)

.PHONY: build install add-go-path test test-fast test-e2e lint fmt tidy ingest list retrieve query analyze export clean install-completions install-completions-fish install-completions-bash install-completions-zsh
