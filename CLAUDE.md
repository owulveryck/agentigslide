# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Google Slides template analysis and presentation generation system that uses Claude Vision (via Vertex AI) to analyze slide templates and programmatically create presentations via the Google Slides/Drive APIs. The system extracts OCTO template slides, analyzes their editable elements and visual components, then assembles and customizes presentations from user requests.

## Core Architecture

### Four-Phase Workflow

1. **Analysis Phase** (`cmd/analysis/`, `cmd/analyzeslides/`)
   - Fetches slides from Google Slides API using presentation ID
   - Saves slide content as JSON (`content.json`) and images (`slide.png`)
   - Uses Claude Vision (Opus 4.5) via Vertex AI to analyze slides
   - Generates `analysis.json` with editable elements, visual elements, and metadata

2. **Index Building Phase** (`cmd/buildindex/`)
   - Aggregates all `analysis.json` files into `template_index.json`
   - Extracts keywords for slide search/matching
   - Generates semantic variable names for editable fields

3. **Planification & Production Phase** (`cmd/slidegen/`)
   - Multi-agent pipeline (default): Outliner → Selector → Writers (parallel) → Reviewer with feedback loop
   - Interactive chat mode (default when no file): refine outline before pipeline runs
   - Agent orchestration in `internal/agent/`, coordinated by pure Go orchestrator
   - Duplicates template via Drive API, applies modifications via Slides BatchUpdate
   - Supports markdown (bold, italic, bullet lists) in text content

4. **Post-production Phase** (`cmd/fixfonts/`) *(optional)*
   - Detects and corrects formatting issues (fonts, sizes, spacing) via AI vision

### Key Components

- **Vertex AI Integration**: All Claude API calls go through Google Cloud Vertex AI endpoints (not direct Anthropic API)
- **Google Slides/Drive API**: Used to fetch templates, duplicate presentations, and apply modifications

## Environment Variables

Configuration is managed via `kelseyhightower/envconfig` with per-package prefixes. Each CLI supports `-h` to list all required/optional variables with defaults.

### Shared variables (used by most CLIs)

```bash
# SLIDES prefix (internal/config)
export SLIDES_TEMPLATE_ID="YOUR_TEMPLATE_PRESENTATION_ID"
export SLIDES_TEMPLATE_INDEX="template_index.json"  # default
export SLIDES_CREDENTIALS="/path/to/oauth2-credentials.json"
export SLIDES_MAX_PARALLEL=5                           # default, concurrent slide-processing goroutines

# VERTEX prefix (internal/vertex)
export VERTEX_PROJECT_ID="your-gcp-project-id"
export VERTEX_REGION="us-east5"  # default
```

### CLI-specific variables (model names, max tokens)

```bash
export SLIDEGEN_MODEL="claude-opus-4-6"              # default, for slidegen (amend mode only)
export ANALYZE_MODEL="claude-opus-4-5@20251101"       # default, for analyzeslides
export ANALYZE_MAX_TOKENS=8192                        # default
export FIXFONTS_MODEL="claude-opus-4-6"               # default, for fixfonts
export FIXFONTS_MAX_TOKENS=16384                      # default
```

### Multi-agent pipeline variables (AGENT prefix)

```bash
export AGENT_OUTLINER_MODEL="claude-sonnet-4-6"              # default
export AGENT_SELECTOR_MODEL="claude-sonnet-4-6"              # default
export AGENT_WRITER_MODEL="claude-sonnet-4-6"                # default, complex slides (>2 fields)
export AGENT_WRITER_SIMPLE_MODEL="claude-haiku-4-5@20251001" # default, simple slides (<=2 fields)
export AGENT_OUTLINER_MAX_TOKENS=32768                         # default, max output tokens for outliner
export AGENT_REVIEWER_MODEL="claude-opus-4-6"                # default
export AGENT_MAX_PARALLEL=3                                   # default, max concurrent writers
export AGENT_DESIGNER_MODEL="claude-sonnet-4-6"               # default, diagram Designer agent
export AGENT_MAX_REVIEW_RETRIES=2                             # default
export AGENT_MAX_SELECTOR_RETRIES=2                           # default, retries on validation failure
export AGENT_DIAGRAM_VISUAL_REVIEW_MODEL="claude-sonnet-4-6"  # default, visual review of diagram slides
export AGENT_MAX_DIAGRAM_VISUAL_RETRIES=1                     # default, 0 to disable visual review
export AGENT_EDIT_PLANNER_MODEL="claude-opus-4-6"             # default, structural edit planning
export AGENT_EDIT_WRITER_MODEL="claude-sonnet-4-6"            # default, complex edits (>2 modifications)
export AGENT_EDIT_WRITER_SIMPLE_MODEL="claude-haiku-4-5@20251001" # default, simple edits (<=2 modifications)
export AGENT_EDIT_REVIEWER_MODEL="claude-opus-4-6"            # default, edit quality validation
export AGENT_EDIT_REVIEW_ENABLED=false                        # default, enable edit reviewer step
export AGENT_MAX_EDIT_REVIEW_RETRIES=1                        # default
export AGENT_EDIT_VISUAL_REVIEW_ENABLED=true                  # default, visual review of edited slides
export AGENT_EDIT_VISUAL_REVIEW_MODEL="claude-sonnet-4-6"     # default, model for edit visual review
export AGENT_MAX_EDIT_VISUAL_RETRIES=1                        # default, max visual feedback iterations (0 = review only)
export AGENT_EDIT_FIXFONTS_ENABLED=true                       # default, run fixfonts on modified slides
```

## Common Commands

### Development Workflow

```bash
# 1. Extract slide content from Google Slides
go run cmd/analysis/main.go

# 2. Analyze specific slides with Claude Vision
go run cmd/analyzeslides/analyze_slides.go --slides 1,2,5,10,20,30,40,50

# 3. Build the searchable index
go run cmd/buildindex/build_template_index.go

# 4. Interactive chat mode (default: refine outline, then generate)
go run cmd/slidegen/main.go

# 4b. Generate directly from a markdown file (skips interactive chat)
go run cmd/slidegen/main.go --file request.md --credentials ~/.config/gcloud/slideappscripter-client.json
```

## Directory Structure

```
template/{PRESENTATION_ID}/{slide_number}/
  ├── content.json      # Raw Google Slides API response
  ├── slide.png         # Visual preview of slide
  ├── analysis.json     # Claude Vision analysis (structured)
  └── analysis.md       # Human-readable analysis
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
- **category**: Semantic classification (couverture, intercalaire, contenu_texte, contenu_illustre, donnees_tableau, donnees_graphique, citation, equipe, timeline, diagramme, conclusion, question)
- **useCaseTags**: 3-5 use-case tags describing when a presenter would choose this slide
- **visualStyle**: Visual style (minimal, illustre, data, pleine_image, split)

### Variable Naming Convention

Variable names for editable fields are generated semantically based on:
1. Element role (extracted from Claude's description: "titre principal" → "titleMain")
2. Position on slide (only added if multiple elements share same role)
3. Convention: `{role}{position}Shape` (e.g., `titleMainShape`, `yearBottomLeftShape`)

### ObjectID Handling

When duplicating slides via `DuplicateObject`, the system uses a predictable ID mapping pattern (`d{counter}_{originalID}`) to maintain control over new ObjectIDs. This allows direct BatchUpdate modifications without needing position-based element detection.

## Module Structure

This is a Go module with module path `github.com/owulveryck/agentigslide`.

## Go Version

Uses Go 1.24.0 (cutting edge). If you encounter compatibility issues with new Go features, be aware this uses the latest Go release.
