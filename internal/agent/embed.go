package agent

import _ "embed"

//go:embed prompt_outliner.txt
var outlinerSystemPrompt string

//go:embed prompt_selector.txt
var selectorSystemPrompt string

//go:embed prompt_writer.txt
var writerSystemPrompt string

//go:embed prompt_reviewer.txt
var reviewerSystemPrompt string
