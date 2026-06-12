package trace

import "time"

// TraceFile is the top-level JSON structure written to disk.
type TraceFile struct {
	Version      string              `json:"version"`
	GeneratedAt  time.Time           `json:"generatedAt"`
	DurationMs   int64               `json:"durationMs"`
	Config       ConfigTrace         `json:"config"`
	UserRequest  string              `json:"userRequest"`
	Phases       []PhaseTrace        `json:"phases,omitempty"`
	Outline      *OutlineTrace       `json:"outline,omitempty"`
	Selection    *SelectionTrace     `json:"selection,omitempty"`
	Writers      []WriterTrace       `json:"writers,omitempty"`
	Review       *ReviewTrace        `json:"review,omitempty"`
	Execution    *ExecutionTrace     `json:"execution,omitempty"`
	Formatter    []FormatterTrace    `json:"formatter,omitempty"`
	VisualReview []VisualReviewTrace `json:"visualReview,omitempty"`
	AgentCalls   []AgentCallTrace    `json:"agentCalls,omitempty"`
	Errors       []ErrorEntry        `json:"errors,omitempty"`
}

// AgentCallTrace is the per-LLM-call ledger entry: one line per API call with
// the model actually used and the full token breakdown (including cache).
// This is the authoritative source for offline cost computation — the
// per-phase token fields elsewhere in the trace are kept for readability but
// undercount (visual review, memory synthesis, designer).
type AgentCallTrace struct {
	Agent            string `json:"agent"`
	Model            string `json:"model"`
	InputTokens      int    `json:"inputTokens"`
	OutputTokens     int    `json:"outputTokens"`
	CacheReadTokens  int    `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int    `json:"cacheWriteTokens,omitempty"`
	DurationMs       int64  `json:"durationMs,omitempty"`
}

// PhaseTrace records the wall-clock window of one pipeline phase so the full
// run duration can be attributed (outline, selection, writers, review,
// execution, formatter-N, visual-review, memory-synthesis).
type PhaseTrace struct {
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
}

type ConfigTrace struct {
	TemplateID          string `json:"templateId"`
	OutlinerModel       string `json:"outlinerModel"`
	SelectorModel       string `json:"selectorModel"`
	WriterModel         string `json:"writerModel"`
	WriterSimpleModel   string `json:"writerSimpleModel"`
	ReviewerModel       string `json:"reviewerModel"`
	DesignerModel       string `json:"designerModel"`
	MaxParallel         int    `json:"maxParallel"`
	MaxReviewRetries    int    `json:"maxReviewRetries"`
	MaxOutlinerRetries  int    `json:"maxOutlinerRetries"`
	MaxSelectorRetries  int    `json:"maxSelectorRetries"`
	FormatterEnabled    bool   `json:"formatterEnabled"`
	VisualReviewEnabled bool   `json:"visualReviewEnabled"`
	MaxVisualRetries    int    `json:"maxVisualRetries"`
	PipelineTimeout     string `json:"pipelineTimeout"`
	ExecutionTimeout    string `json:"executionTimeout,omitempty"`
	VisualReviewTimeout string `json:"visualReviewTimeout,omitempty"`
	FormatterTimeout    string `json:"formatterTimeout,omitempty"`
}

type OutlineTrace struct {
	InputSummary  string           `json:"inputSummary"`
	Attempts      []OutlineAttempt `json:"attempts"`
	FinalSections []SectionSummary `json:"finalSections"`
}

type OutlineAttempt struct {
	Attempt         int    `json:"attempt"`
	ValidationError string `json:"validationError,omitempty"`
	DurationMs      int64  `json:"durationMs"`
	TokensIn        int    `json:"tokensIn"`
	TokensOut       int    `json:"tokensOut"`
}

type SectionSummary struct {
	Title      string             `json:"title"`
	Purpose    string             `json:"purpose"`
	SlideNeeds []SlideNeedSummary `json:"slideNeeds"`
}

type SlideNeedSummary struct {
	Intent        string   `json:"intent"`
	ItemCount     int      `json:"itemCount"`
	MaxItemLength int      `json:"maxItemLength"`
	SlideType     string   `json:"slideType"`
	NeedsTitle    bool     `json:"needsTitle"`
	NeedsSubtitle bool     `json:"needsSubtitle"`
	ContentItems  []string `json:"contentItems"`
}

type SelectionTrace struct {
	Attempts []SelectionAttempt `json:"attempts"`
	Final    []SelectionEntry   `json:"final"`
}

type SelectionAttempt struct {
	Attempt         int    `json:"attempt"`
	ValidationError string `json:"validationError,omitempty"`
	DurationMs      int64  `json:"durationMs"`
	TokensIn        int    `json:"tokensIn"`
	TokensOut       int    `json:"tokensOut"`
}

type SelectionEntry struct {
	Index          int            `json:"index"`
	SourceSlide    int            `json:"sourceSlide"`
	Rationale      string         `json:"rationale"`
	TemplateFields []FieldSummary `json:"templateFields"`
}

type FieldSummary struct {
	VariableName string `json:"variableName"`
	Role         string `json:"role"`
	MaxChars     int    `json:"maxChars"`
}

type WriterTrace struct {
	SlideIndex  int                 `json:"slideIndex"`
	SourceSlide int                 `json:"sourceSlide"`
	ModelUsed   string              `json:"modelUsed"`
	SlideType   string              `json:"slideType"`
	DurationMs  int64               `json:"durationMs"`
	TokensIn    int                 `json:"tokensIn"`
	TokensOut   int                 `json:"tokensOut"`
	Input       WriterInput         `json:"input"`
	Output      WriterOutput        `json:"output"`
	Enforcement []EnforcementAction `json:"enforcement,omitempty"`
	Feedback    []FeedbackEntry     `json:"feedback,omitempty"`
}

type WriterInput struct {
	Intent       string         `json:"intent"`
	ContentItems []string       `json:"contentItems"`
	Fields       []FieldSummary `json:"fields"`
}

type WriterOutput struct {
	Modifications []ModificationTrace `json:"modifications"`
}

type ModificationTrace struct {
	VariableName string `json:"variableName"`
	NewText      string `json:"newText"`
	CharCount    int    `json:"charCount"`
	MaxChars     int    `json:"maxChars"`
	OverLimit    bool   `json:"overLimit"`
}

type EnforcementAction struct {
	VariableName   string `json:"variableName"`
	OriginalLength int    `json:"originalLength"`
	TruncatedTo    int    `json:"truncatedTo"`
	MaxChars       int    `json:"maxChars"`
}

type FeedbackEntry struct {
	IssueType   string `json:"issueType"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
	Field       string `json:"field,omitempty"`
}

type ReviewTrace struct {
	Iterations []ReviewIteration `json:"iterations"`
}

type ReviewIteration struct {
	Attempt    int                `json:"attempt"`
	Approved   bool               `json:"approved"`
	IssueCount int                `json:"issueCount"`
	Issues     []ReviewIssueTrace `json:"issues,omitempty"`
	// DroppedIssues are reviewer findings discarded by the deterministic
	// cross-check (ADR 030): false positives on computable facts.
	DroppedIssues   []ReviewIssueTrace `json:"droppedIssues,omitempty"`
	CorrectedSlides []int              `json:"correctedSlides,omitempty"`
	DurationMs      int64              `json:"durationMs"`
	TokensIn        int                `json:"tokensIn"`
	TokensOut       int                `json:"tokensOut"`
}

type ReviewIssueTrace struct {
	SlideIndex  int    `json:"slideIndex"`
	Field       string `json:"field,omitempty"`
	IssueType   string `json:"issueType"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

type ExecutionTrace struct {
	PresentationID string                `json:"presentationId"`
	SlidesCreated  int                   `json:"slidesCreated"`
	DurationMs     int64                 `json:"durationMs,omitempty"`
	PerSlide       []SlideExecutionTrace `json:"perSlide"`
}

type SlideExecutionTrace struct {
	PlanIndex      int                       `json:"planIndex"`
	SourceSlideID  string                    `json:"sourceSlideId"`
	NewPageID      string                    `json:"newPageId"`
	IsDiagram      bool                      `json:"isDiagram"`
	BaseStyles     map[string]BaseStyleTrace `json:"baseStyles,omitempty"`
	ElementMap     map[string]string         `json:"elementMap,omitempty"`
	TextInsertions []TextInsertionTrace      `json:"textInsertions,omitempty"`
}

type BaseStyleTrace struct {
	FontFamily string  `json:"fontFamily,omitempty"`
	FontSizePt float64 `json:"fontSizePt,omitempty"`
	FgColorHex string  `json:"fgColorHex,omitempty"`
}

type TextInsertionTrace struct {
	ElementID       string `json:"elementId"`
	VariableName    string `json:"variableName,omitempty"`
	TextLength      int    `json:"textLength"`
	HadExistingText bool   `json:"hadExistingText"`
}

type FormatterTrace struct {
	Pass         int                        `json:"pass"`
	IssueCount   int                        `json:"issueCount"`
	AppliedCount int                        `json:"appliedCount"`
	DurationMs   int64                      `json:"durationMs,omitempty"`
	Issues       []FormatterIssueTrace      `json:"issues,omitempty"`
	Corrections  []FormatterCorrectionTrace `json:"corrections,omitempty"`
}

type FormatterIssueTrace struct {
	Rule       string `json:"rule"`
	SlideIndex int    `json:"slideIndex"`
	ObjectID   string `json:"objectId"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Severity   string `json:"severity"`
}

type FormatterCorrectionTrace struct {
	ObjectID   string `json:"objectId"`
	SlideIndex int    `json:"slideIndex"`
	Type       string `json:"type"`
	Reason     string `json:"reason"`
}

type VisualReviewTrace struct {
	Attempt     int                  `json:"attempt"`
	DurationMs  int64                `json:"durationMs,omitempty"`
	Findings    []VisualFindingTrace `json:"findings"`
	Corrections int                  `json:"correctionsApplied"`
}

type VisualFindingTrace struct {
	PageID           string             `json:"pageId"`
	Approved         bool               `json:"approved"`
	ThumbnailFetchMs int64              `json:"thumbnailFetchMs,omitempty"`
	ReviewMs         int64              `json:"reviewMs,omitempty"`
	Issues           []VisualIssueTrace `json:"issues,omitempty"`
}

type VisualIssueTrace struct {
	IssueType   string `json:"issueType"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

type ErrorEntry struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}
