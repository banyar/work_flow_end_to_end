# Send a ticket JSON through the RT Web UI flow (section 1, step 1-5).
#   make send
#   make send FILE=ticket.json
#   make send RUN_ID=<uuid>
#   make send ARGS='--set id=97 --set custom_fields.service_type="MNet Plus"'
#
# Report criteria samples (see criteria/README.md).
#   make criteria-mock               # Node-RED mock on :3002 (point NODE_RED_BASE_URL at it)
#   make criteria-mock-stop          # stop the mock container
#   make send-criteria C=20_kept_olt_offline
#   make criteria-ui                 # web UI on http://127.0.0.1:8090 (ticket id + criteria + cpe id)
#
# Check which systems the flow depends on are not ready (read-only).
#   make check
#   make up                          # start what is down (containers, mock, noc_automation, rtutil, criteria UI, pipeline_report), then check
#   make down                        # stop only what make up started
#   make up NOC_ROOT=<NocAutomationCodeMerge path>
#   make up MODE=sit                 # MODE=local|sit|qa picks rtutil's config:
#                                    #   local .rtutil_local.json · sit .rtutil.json · qa .rtutil_qa.json
#                                    #   (RTUTIL_CONFIG=<file> overrides; a running rtutil is restarted on change)
#
# Docker (criteria UI + criteria mock; needs .env, see docker-compose.yml).
#   make docker-up                   # vendor, build and start in the background
#   make docker-logs                 # follow the UI log
#   make docker-send-criteria C=20_kept_olt_offline
#   make docker-down
FILE ?= ticket_valid.json
ENV ?= .env
RUN_ID ?=
ARGS ?=

.PHONY: send
send:
	go run . send --file $(FILE) --env $(ENV) $(if $(RUN_ID),--run-id $(RUN_ID)) $(ARGS)

CRITERIA_DIR ?= criteria
CRITERIA_MOCK_PORT ?= 3002
C ?=
UI_ADDR ?= 127.0.0.1:8091
NOC_ROOT ?= ../../NocAutomationCodeMerge
NOC_DIR ?= $(NOC_ROOT)/noc_automation
NOC_ENV ?= $(NOC_DIR)/.env
RTUTIL_DIR ?= $(NOC_ROOT)/remote-resolved-queue-transfer
REPORT_DIR ?= ../../pipeline_report
MODE ?= local
RTUTIL_CONFIG ?=
UP_ARGS = --env $(ENV) --noc-env $(NOC_ENV) --noc-dir $(NOC_DIR) --rtutil-dir $(RTUTIL_DIR) \
	--mock-port $(CRITERIA_MOCK_PORT) --mock-data $(CRITERIA_DIR)/mockoon-criteria.json \
	--ui-addr $(UI_ADDR) --criteria $(CRITERIA_DIR)/tickets --report-dir $(REPORT_DIR) \
	--mode $(MODE) $(if $(RTUTIL_CONFIG),--rtutil-config $(RTUTIL_CONFIG))

.PHONY: criteria-mock
criteria-mock:
	docker run --rm --name noc-criteria-mock -p 127.0.0.1:$(CRITERIA_MOCK_PORT):3002 \
		-v $(abspath $(CRITERIA_DIR))/mockoon-criteria.json:/data/mock.json:ro \
		mockoon/cli:9.9.0 --data /data/mock.json --port 3002

.PHONY: criteria-mock-stop
criteria-mock-stop:
	-docker stop noc-criteria-mock

.PHONY: send-criteria
send-criteria:
	@test -n "$(C)" || { echo "usage: make send-criteria C=<file name without .json>"; ls $(CRITERIA_DIR)/tickets | sed 's/\.json$$//'; exit 3; }
	@test -f $(CRITERIA_DIR)/tickets/$(C).json || { echo "no $(CRITERIA_DIR)/tickets/$(C).json"; exit 3; }
	@$(MAKE) --no-print-directory send FILE=$(CRITERIA_DIR)/tickets/$(C).json

.PHONY: criteria-ui
criteria-ui:
	go run . serve --env $(ENV) --addr $(UI_ADDR) --criteria $(CRITERIA_DIR)/tickets

# Build rt_web_ui into a temp file and run it, so go run's "exit status N" noise stays out.
define run_tool
	@command -v go >/dev/null || { echo " ✘ Go                  not installed → https://go.dev/dl (1.24+)"; exit 1; }
	@bin=$$(mktemp); \
	go build -o $$bin . || { rm -f $$bin; echo " ✘ Go build            failed (see errors above) → go mod download / rtdatacore version စစ်ပါ"; exit 1; }; \
	$$bin $(1); rc=$$?; rm -f $$bin; exit $$rc
endef

.PHONY: check
check:
	$(call run_tool,check --env $(ENV) --noc-env $(NOC_ENV) --mock-port $(CRITERIA_MOCK_PORT) --ui-addr $(UI_ADDR) --report-dir $(REPORT_DIR) \
		--mode $(MODE) --rtutil-dir $(RTUTIL_DIR) $(if $(RTUTIL_CONFIG),--rtutil-config $(RTUTIL_CONFIG)))

.PHONY: up
up:
	$(call run_tool,up $(UP_ARGS))

.PHONY: down
down:
	$(call run_tool,down $(UP_ARGS))

.PHONY: test
test:
	go test ./...

# git.frontiir.net modules are private: vendor them on the host so the image
# builds without git credentials.
.PHONY: vendor
vendor:
	go mod vendor

.PHONY: docker-build
docker-build: vendor
	docker compose build

.PHONY: docker-up
docker-up: vendor
	@test -f $(ENV) || { echo "no $(ENV) → cp .env.example .env and fill it in"; exit 3; }
	UI_ADDR=$(UI_ADDR) CRITERIA_MOCK_PORT=$(CRITERIA_MOCK_PORT) docker compose up -d --build
	@echo "criteria UI on http://$(UI_ADDR) · mock on :$(CRITERIA_MOCK_PORT)"

.PHONY: docker-down
docker-down:
	docker compose down

.PHONY: docker-logs
docker-logs:
	docker compose logs -f rt-web-ui

.PHONY: docker-send-criteria
docker-send-criteria:
	@test -n "$(C)" || { echo "usage: make docker-send-criteria C=<file name without .json>"; exit 3; }
	docker compose exec rt-web-ui /app/rt-web-ui send --env /app/.env --file /app/criteria/tickets/$(C).json $(ARGS)
