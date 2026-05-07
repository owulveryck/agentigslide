// Package templateindex provides business logic for building a searchable
// template index from analyzed Google Slides. It processes slide analysis
// results (produced by Claude Vision) and raw slide content (from Google
// Slides API) to extract keywords, infer semantic roles, generate variable
// names, compute field dimensions, estimate character capacity, resolve
// table cell mappings, and detect layout structure.
//
// The main entry point is [BuildIndex], which aggregates a slice of
// [model.SlideAnalysis] into a [model.TemplateIndex] suitable for
// downstream slide generation.
package templateindex
