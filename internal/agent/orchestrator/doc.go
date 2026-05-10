// Package orchestrator coordinates the multi-agent pipeline for
// presentation generation.
//
// The pipeline consists of five steps:
//  1. Outliner — produces a structured PresentationOutline
//  2. Selector — maps slide needs to template slides
//  3. Writers — generates content for each slide (parallel)
//  4. Assembler — combines writer outputs into a GenerationPlan
//  5. Reviewer — validates quality with optional retry loop
//
// The orchestrator uses agents from the sibling sub-packages
// (outliner, selector, writer, reviewer) and validation logic from
// the parent agent package.
package orchestrator
