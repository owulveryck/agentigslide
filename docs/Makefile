PLANTUML ?= plantuml

SOURCES := $(wildcard *.puml)
SVGS    := $(SOURCES:.puml=.svg)
PNGS    := $(SOURCES:.puml=.png)

.DEFAULT_GOAL := all

all: svg png

svg: $(SVGS)
png: $(PNGS)

%.svg: %.puml
	$(PLANTUML) -tsvg $<

%.png: %.puml
	$(PLANTUML) -tpng $<

clean:
	rm -f $(SVGS) $(PNGS)

list:
	@echo "Sources: $(SOURCES)"
	@echo "SVGs:    $(SVGS)"
	@echo "PNGs:    $(PNGS)"

.PHONY: all svg png clean list
