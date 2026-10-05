package agui

import "encoding/json"

type TDesignSuggestion struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

// SuggestionTool 用于在TDesignChat上生成建议(调用TDesign内置的Tool "suggestion")
func (e *Emitter) SuggestionTool(message string, suggestions []TDesignSuggestion) {
	suggestionToolCallID := e.ToolStartWithParentMessageID("suggestion", message)
	e.ToolEnd(suggestionToolCallID)
	suggestionsJSON, _ := json.Marshal(suggestions)
	e.ToolResult(message, suggestionToolCallID, string(suggestionsJSON))

}
