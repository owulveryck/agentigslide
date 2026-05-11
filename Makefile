GO       ?= go
GOFLAGS  ?=
LDFLAGS  ?=

BINDIR   := bin
PARALLEL ?= 5

SHARED_SOURCES := $(shell find internal/ markdown/ -type f -name '*.go' 2>/dev/null)
MODULE_FILES   := go.mod go.sum

BINARIES := \
	$(BINDIR)/analysis \
	$(BINDIR)/analyzeSlides \
	$(BINDIR)/buildTemplateIndex \
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

$(BINDIR)/analysis: $(wildcard analysis/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./analysis/

$(BINDIR)/analyzeSlides: $(wildcard analyzeSlides/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./analyzeSlides/

$(BINDIR)/buildTemplateIndex: $(wildcard buildTemplateIndex/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./buildTemplateIndex/

$(BINDIR)/slidegen: $(wildcard slidegen/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./slidegen/

$(BINDIR)/fixfonts: $(wildcard fixfonts/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./fixfonts/

$(BINDIR)/mcp-server: $(wildcard mcp-server/*.go) $(SHARED_SOURCES) $(MODULE_FILES) | $(BINDIR)
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

template: $(BINDIR)/analysis $(BINDIR)/analyzeSlides $(BINDIR)/buildTemplateIndex
ifndef SLIDES_TEMPLATE_ID
	$(warning ****************************************************************)
	$(warning  WARNING: SLIDES_TEMPLATE_ID is not set.)
	$(warning  This variable must contain the Google Slides presentation ID.)
	$(warning  Example: export SLIDES_TEMPLATE_ID="1MycsjRBQ67mWJ...")
	$(warning ****************************************************************)
	$(error Aborting: SLIDES_TEMPLATE_ID is required for template generation)
endif
	@echo "=== Phase 1: Fetching slide content ==="
	$(BINDIR)/analysis
	@echo ""
	@echo "=== Phase 2: Analyzing slides with Claude Vision ($(PARALLEL) in parallel) ==="
	@SLIDES=$$(ls -1 template/$(SLIDES_TEMPLATE_ID)/ 2>/dev/null \
		| grep -E '^[0-9]+$$' \
		| sort -n); \
	if [ -z "$$SLIDES" ]; then \
		echo "ERROR: No slide directories found under template/$(SLIDES_TEMPLATE_ID)/"; \
		echo "Phase 1 (analysis) may have failed."; \
		exit 1; \
	fi; \
	TOTAL=$$(echo "$$SLIDES" | wc -l | tr -d ' '); \
	echo "Analyzing $$TOTAL slides ($(PARALLEL) parallel workers)..."; \
	echo "$$SLIDES" | xargs -P $(PARALLEL) -I{} sh -c \
		'echo "[slide {}] start"; $(BINDIR)/analyzeSlides --slides {} && echo "[slide {}] done" || { echo "[slide {}] FAILED"; exit 1; }'
	@echo ""
	@echo "=== Phase 3: Building template index ==="
	$(BINDIR)/buildTemplateIndex
	@echo ""
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
	@echo "  make template     Run full template analysis pipeline (requires SLIDES_TEMPLATE_ID)"
	@echo "                    Use PARALLEL=N to control concurrency (default: $(PARALLEL))"
	@echo "  make test         Run all tests"
	@echo "  make vet          Run go vet"
	@echo "  make fmt          Format all Go source files"
	@echo "  make lint         Run vet + staticcheck"
	@echo "  make clean        Remove bin/ and Go caches"
	@echo "  make help         Show this help"
	@echo ""
	@echo "Binaries: $(notdir $(BINARIES))"
	@echo "Agents:   $(notdir $(AGENTS))"

.PHONY: all agents test vet fmt lint clean template help
