# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Google Slides template analysis and generation system that uses Claude Vision (via Vertex AI) to analyze slide templates and generate Google Apps Script code for automated presentation creation. The system extracts OCTO template slides, analyzes their editable elements and visual components, and provides tools to programmatically create presentations.

## Core Architecture

### Three-Phase Workflow

1. **Analysis Phase** (`analysis/`, `analyzeSlides/`)
   - Fetches slides from Google Slides API using presentation ID
   - Saves slide content as JSON (`content.json`) and images (`slide.png`)
   - Uses Claude Vision (Opus 4.5) via Vertex AI to analyze slides
   - Generates `analysis.json` with editable elements, visual elements, and metadata
   - Generates `analysis.md` (human-readable) and `appscript.js` (Apps Script snippets)

2. **Index Building Phase** (`buildTemplateIndex/`)
   - Aggregates all `analysis.json` files into `template_index.json`
   - Extracts keywords for slide search/matching
   - Generates semantic variable names and update functions for Apps Script

3. **Generation Phase** (`generateAppScript/`)
   - Takes user requests for presentation creation
   - Uses Claude (Sonnet 4.5) to parse requests and match slides
   - Generates complete Google Apps Script code ready to copy/paste

### Key Components

- **Vertex AI Integration**: All Claude API calls go through Google Cloud Vertex AI endpoints (not direct Anthropic API)
- **Google Slides API**: Used to fetch original slide content and metadata
- **Apps Script Code Generation**: Generates position/content-based element detection (ObjectIDs change on copy)
- **MCP Server** (`mcp-server/`): Work-in-progress MCP server for Claude Desktop integration

## Environment Variables

Required for all operations:
```bash
export SLIDES_PREFORMATES_ID="1MycsjRBQ67mWJ0SxlAgY4A_J04RluDsH8kgsCpixVwI"
export ANTHROPIC_VERTEX_PROJECT_ID="your-gcp-project-id"
export CLOUD_ML_REGION="us-east5"  # or "global"
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/credentials.json"
```

## Common Commands

### Development Workflow

```bash
# 1. Extract slide content from Google Slides
go run analysis/main.go

# 2. Analyze specific slides with Claude Vision
go run analyzeSlides/analyze_slides.go --slides 1,2,5,10,20,30,40,50

# 3. Build the searchable index
go run buildTemplateIndex/build_template_index.go

# 4. Generate Apps Script from user request
go run generateAppScript/generate_appscript.go --request "Create a deck 'Innovation' with title slide"

# Interactive mode for multi-line requests
go run generateAppScript/generate_appscript.go --interactive

# 5. Generate a complete presentation from a markdown file (recommended)
go run slidegen/main.go --file request.md --credentials ~/.config/gcloud/slideappscripter-client.json

# 6. Generate structured slide list (JSON) from user request
go run generateSlideList/generate_slide_list.go --request "Create a deck 'Innovation' with title slide"

# Interactive mode for multi-line requests
go run generateSlideList/generate_slide_list.go --interactive

# 7. Apply a slide list plan to create the actual presentation
go run applySlideList/apply_slide_list.go --plan plan.json

# Or pipe directly from generateSlideList
go run generateSlideList/generate_slide_list.go --request "..." | go run applySlideList/apply_slide_list.go --plan -
```

### PDF Extraction (Optional)

```bash
# Extract all slides as PNG from PDF (requires pdftoppm)
go run extractPDF/extract_pdf.go
```

### MCP Server (Work in Progress)

```bash
cd mcp-server
go run main.go
```

## Directory Structure

```
template/{PRESENTATION_ID}/{slide_number}/
  ├── content.json      # Raw Google Slides API response
  ├── slide.png         # Visual preview of slide
  ├── analysis.json     # Claude Vision analysis (structured)
  ├── analysis.md       # Human-readable analysis
  └── appscript.js      # Apps Script helper functions for this slide
```

## Important Implementation Details

### Vertex AI Authentication

The codebase uses Google Cloud default credentials for Vertex AI API calls. The authentication pattern is:
1. Load credentials via `google.FindDefaultCredentials()`
2. Create HTTP client with credentials using `htransport.NewClient()`
3. Manually construct Vertex AI endpoint URLs and make HTTP POST requests

Do NOT use the Anthropic SDK directly - all API calls must go through Vertex AI endpoints.

### Claude Vision Analysis Structure

When analyzing slides, Claude Vision returns structured JSON with:
- **editableElements**: Text elements that can be modified (with ObjectIDs mapped from content.json)
- **visualElements**: Reusable visual components (icons, images, logos) with ObjectIDs for copying

### Apps Script Variable Naming

Variable names are generated semantically based on:
1. Element role (extracted from Claude's description: "titre principal" → "titleMain")
2. Position on slide (only added if multiple elements share same role)
3. Convention: `{role}{position}Shape` (e.g., `titleMainShape`, `yearBottomLeftShape`)

### ObjectID Handling Critical Note

Google Slides generates NEW ObjectIDs when copying slides via `appendSlide()`. The generated Apps Script code uses position, size, placeholder type, and content matching to identify elements after copying - NOT ObjectIDs from the template.

## Module Structure

This is a multi-module Go project:
- Root module: `example.com` (main analysis and generation tools)
- MCP server: Separate module at `mcp-server/` with its own `go.mod`

When adding dependencies, ensure you're in the correct directory.

## Go Version

Uses Go 1.24.0 (cutting edge). If you encounter compatibility issues with new Go features, be aware this uses the latest Go release.
