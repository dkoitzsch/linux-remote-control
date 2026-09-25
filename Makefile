.PHONY: all frontend agent clean

BIN_DIR := bin

all: frontend agent

# Builds the React portal and copies it into the Go module so `go:embed`
# picks it up. Must run before `agent` for the real UI to be included
# (agent/internal/webui/dist otherwise still contains the placeholder page).
frontend:
	cd web && npm install && npm run build
	rm -rf agent/internal/webui/dist
	cp -r web/dist agent/internal/webui/dist

agent:
	mkdir -p $(BIN_DIR)
	cd agent && go build -o ../$(BIN_DIR)/ ./...

clean:
	rm -rf $(BIN_DIR) web/dist
