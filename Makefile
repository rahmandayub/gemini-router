BINARY_NAME := gemini-router

INSTALL_DIR := $(HOME)/.local/bin
CONFIG_DIR := $(HOME)/.config/gemini-router
SERVICE_DIR := $(HOME)/.config/systemd/user

CONFIG_DST := $(CONFIG_DIR)/config.yaml

SERVICE_FILE := $(SERVICE_DIR)/$(BINARY_NAME).service

DEFAULT_PORT := 18080

.PHONY: build install uninstall clean

build:
	go build -o $(BINARY_NAME) ./cmd/gemini-router

install: build
	@echo ""
	@echo "=== Gemini Router Installer ==="
	@echo ""

	@echo "Installing binary to $(INSTALL_DIR)..."
	@systemctl --user stop $(BINARY_NAME) 2>/dev/null || true
	@mkdir -p $(INSTALL_DIR)
	@cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_DIR)/$(BINARY_NAME)

	@mkdir -p $(CONFIG_DIR)
	@if [ -f $(CONFIG_DST) ]; then \
		echo ""; \
		echo "Config already exists at $(CONFIG_DST)"; \
		read -p "Overwrite? [y/N] " overwrite; \
		if [ "$$overwrite" != "y" ] && [ "$$overwrite" != "Y" ]; then \
			echo "Keeping existing config."; \
			skip_config=1; \
		fi; \
	fi; \
	if [ -z "$$skip_config" ]; then \
		echo ""; \
		echo "--- Configuration Setup ---"; \
		echo ""; \
		read -p "Enter port [$(DEFAULT_PORT)]: " port; \
		port=$${port:-$(DEFAULT_PORT)}; \
		echo ""; \
		echo "Enter API keys (empty line to finish):"; \
		echo 'server:' > $(CONFIG_DST); \
		echo '    host: "127.0.0.1"' >> $(CONFIG_DST); \
		echo "    port: $$port" >> $(CONFIG_DST); \
		echo '' >> $(CONFIG_DST); \
		echo 'gemini:' >> $(CONFIG_DST); \
		echo '    base_url: "https://generativelanguage.googleapis.com"' >> $(CONFIG_DST); \
		echo '    api_keys:' >> $(CONFIG_DST); \
		keys_count=0; \
		while true; do \
			read -p "  API key: " key; \
			if [ -z "$$key" ]; then \
				break; \
			fi; \
			echo "        - '$$key'" >> $(CONFIG_DST); \
			keys_count=$$((keys_count + 1)); \
			echo "  Key added"; \
		done; \
		if [ $$keys_count -eq 0 ]; then \
			echo "Error: At least one API key is required!"; \
			rm -f $(CONFIG_DST); \
			exit 1; \
		fi; \
		echo '' >> $(CONFIG_DST); \
		echo 'logging:' >> $(CONFIG_DST); \
		echo '    level: "info"' >> $(CONFIG_DST); \
		echo ""; \
		echo "Config saved to $(CONFIG_DST) ($$keys_count keys)"; \
	fi

	@echo ""
	@echo "Installing systemd user service..."
	@mkdir -p $(SERVICE_DIR)
	@echo '[Unit]' > $(SERVICE_FILE)
	@echo 'Description=Gemini Router Proxy' >> $(SERVICE_FILE)
	@echo 'After=network.target' >> $(SERVICE_FILE)
	@echo '' >> $(SERVICE_FILE)
	@echo '[Service]' >> $(SERVICE_FILE)
	@echo 'Type=simple' >> $(SERVICE_FILE)
	@echo "ExecStart=$(INSTALL_DIR)/$(BINARY_NAME) -config $(CONFIG_DST)" >> $(SERVICE_FILE)
	@echo 'Restart=on-failure' >> $(SERVICE_FILE)
	@echo 'RestartSec=5' >> $(SERVICE_FILE)
	@echo '' >> $(SERVICE_FILE)
	@echo '[Install]' >> $(SERVICE_FILE)
	@echo 'WantedBy=default.target' >> $(SERVICE_FILE)

	@systemctl --user daemon-reload
	@systemctl --user enable --now $(BINARY_NAME)
	@echo ""
	@echo "=== Installation Complete ==="
	@echo ""
	@echo "  Binary:  $(INSTALL_DIR)/$(BINARY_NAME)"
	@echo "  Config:  $(CONFIG_DST)"
	@echo "  Service: $(SERVICE_FILE)"
	@echo ""
	@echo "Commands:"
	@echo "  systemctl --user status $(BINARY_NAME)"
	@echo "  systemctl --user restart $(BINARY_NAME)"
	@echo "  journalctl --user -u $(BINARY_NAME) -f"
	@echo ""

uninstall:
	@echo "Stopping and disabling service..."
	@systemctl --user stop $(BINARY_NAME) 2>/dev/null || true
	@systemctl --user disable $(BINARY_NAME) 2>/dev/null || true

	@echo "Removing binary..."
	@rm -f $(INSTALL_DIR)/$(BINARY_NAME)

	@echo "Removing service file..."
	@rm -f $(SERVICE_FILE)
	@systemctl --user daemon-reload

	@echo ""
	@echo "Uninstalled successfully!"
	@echo "Config kept at $(CONFIG_DST)"
	@echo "To remove config: rm -rf $(CONFIG_DIR)"

clean:
	@rm -f $(BINARY_NAME)
