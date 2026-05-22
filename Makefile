GO       ?= go
GOFLAGS  ?=
LDFLAGS  ?=

BINDIR   := bin
EXTRACT_PARALLEL ?= 1
ANALYZE_PARALLEL ?= 10

SHARED_SOURCES := $(shell find internal/ markdown/ -type f -name '*.go' 2>/dev/null)
MODULE_FILES   := go.mod go.sum

BINARIES := \
	$(BINDIR)/analysis \
	$(BINDIR)/analyzeslides \
	$(BINDIR)/buildindex \
	$(BINDIR)/slidegen \
	$(BINDIR)/fixfonts \
	$(BINDIR)/mcp-server

AGENTS := \
	$(BINDIR)/agent_outliner \
	$(BINDIR)/agent_selector \
	$(BINDIR)/agent_writer \
	$(BINDIR)/agent_reviewer \
	$(BINDIR)/agent_orchestrator

.DEFAULT_GOAL := all

all: $(BINARIES)

# ---- Build rules ----

$(BINDIR)/analysis: $(wildcard cmd/analysis/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/analysis/

$(BINDIR)/analyzeslides: $(wildcard cmd/analyzeslides/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/analyzeslides/

$(BINDIR)/buildindex: $(wildcard cmd/buildindex/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/buildindex/

$(BINDIR)/slidegen: $(wildcard cmd/slidegen/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/slidegen/

$(BINDIR)/fixfonts: $(wildcard cmd/fixfonts/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/fixfonts/

$(BINDIR)/mcp-server: $(wildcard exp/mcp-server/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./exp/mcp-server/

# ---- A2A agent binaries ----

agents: $(AGENTS)

$(BINDIR)/agent_outliner: $(wildcard cmd/outliner/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/outliner/

$(BINDIR)/agent_selector: $(wildcard cmd/selector/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/selector/

$(BINDIR)/agent_writer: $(wildcard cmd/writer/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/writer/

$(BINDIR)/agent_reviewer: $(wildcard cmd/reviewer/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/reviewer/

$(BINDIR)/agent_orchestrator: $(wildcard cmd/orchestrator/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/orchestrator/

$(BINDIR):
	mkdir -p $(BINDIR)

# ---- Template generation workflow ----

extract: $(BINDIR)/analysis
	@test -n "$(SLIDES_TEMPLATE_ID)" || { echo "ERROR: SLIDES_TEMPLATE_ID is not set. Example: export SLIDES_TEMPLATE_ID=\"1MycsjRBQ67mWJ...\""; exit 1; }
	@echo "=== Extracting slide content ==="
	$(BINDIR)/analysis
	@echo "=== Extraction complete ==="

analyze: $(BINDIR)/analyzeslides
	@test -n "$(SLIDES_TEMPLATE_ID)" || { echo "ERROR: SLIDES_TEMPLATE_ID is not set. Example: export SLIDES_TEMPLATE_ID=\"1MycsjRBQ67mWJ...\""; exit 1; }
	@echo "=== Analyzing slides with Claude Vision ($(ANALYZE_PARALLEL) in parallel) ==="
	@SLIDES=$$(ls -1 template/$(SLIDES_TEMPLATE_ID)/ 2>/dev/null \
		| grep -E '^[0-9]+$$' \
		| sort -n); \
	if [ -z "$$SLIDES" ]; then \
		echo "ERROR: No slide directories found under template/$(SLIDES_TEMPLATE_ID)/"; \
		echo "Run 'make extract' first."; \
		exit 1; \
	fi; \
	TOTAL=$$(echo "$$SLIDES" | wc -l | tr -d ' '); \
	echo "Analyzing $$TOTAL slides ($(ANALYZE_PARALLEL) parallel workers)..."; \
	echo "$$SLIDES" | xargs -P $(ANALYZE_PARALLEL) -I{} sh -c \
		'echo "[slide {}] start"; $(BINDIR)/analyzeslides --slides {} && echo "[slide {}] done" || { echo "[slide {}] FAILED"; exit 1; }'
	@echo "=== Analysis complete ==="

buildindex: $(BINDIR)/buildindex
	@test -n "$(SLIDES_TEMPLATE_ID)" || { echo "ERROR: SLIDES_TEMPLATE_ID is not set. Example: export SLIDES_TEMPLATE_ID=\"1MycsjRBQ67mWJ...\""; exit 1; }
	@echo "=== Building template index ==="
	$(BINDIR)/buildindex
	@echo "=== Index built ==="

template: extract analyze buildindex
	@echo "=== Template generation complete ==="

# ---- Standard targets ----

test:
	$(GO) test $(GOFLAGS) ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

lint: vet
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed, skipping (go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi

clean:
	rm -rf $(BINDIR)
	$(GO) clean -cache -testcache

help:
	@echo "AgentiGSlide - Build targets:"
	@echo ""
	@echo "  make              Build all binaries into bin/"
	@echo "  make agents       Build all A2A agent servers into bin/"
	@echo "  make bin/<name>   Build a specific binary"
	@echo "  make template     Run full pipeline: extract + analyze + buildindex"
	@echo "  make extract      Fetch slide content from Google Slides (EXTRACT_PARALLEL=$(EXTRACT_PARALLEL))"
	@echo "  make analyze      Analyze all slides with Claude Vision (ANALYZE_PARALLEL=$(ANALYZE_PARALLEL))"
	@echo "  make buildindex   Build the template index from analysis results"
	@echo "  make test         Run all tests"
	@echo "  make vet          Run go vet"
	@echo "  make fmt          Format all Go source files"
	@echo "  make lint         Run vet + staticcheck"
	@echo "  make clean        Remove bin/ and Go caches"
	@echo "  make help         Show this help"
	@echo ""
	@echo "Binaries: $(notdir $(BINARIES))"
	@echo "Agents:   $(notdir $(AGENTS))"

.PHONY: all agents test vet fmt lint clean template extract analyze buildindex help
