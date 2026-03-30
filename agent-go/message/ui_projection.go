package message

import (
	"encoding/json"
	"fmt"
)

// DynamicToolPart represents a tool invocation with its full lifecycle state,
// used only in the UI projection format. It is not a Part variant — it exists
// only in the JSON output of ProjectUIMessages.
type DynamicToolPart struct {
	Type                 string          `json:"type"`
	ToolName             string          `json:"toolName"`
	ToolCallID           string          `json:"toolCallId"`
	State                string          `json:"state"`
	Title                string          `json:"title,omitempty"`
	ProviderExecuted     *bool           `json:"providerExecuted,omitempty"`
	Input                json.RawMessage `json:"input,omitempty"`
	Output               json.RawMessage `json:"output,omitempty"`
	ErrorText            string          `json:"errorText,omitempty"`
	Approval             *ToolApproval   `json:"approval,omitempty"`
	Preliminary          *bool           `json:"preliminary,omitempty"`
	CallProviderMetadata json.RawMessage `json:"callProviderMetadata,omitempty"`
}

func (DynamicToolPart) uiPartType() string { return "dynamic-tool" }

// ToolApproval represents a tool approval request/response within a DynamicToolPart.
type ToolApproval struct {
	ID       string `json:"id"`
	Approved *bool  `json:"approved,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// UIMessage is the JSON wire format for a UIMessage in the AI SDK v6 protocol.
// Parts are marshaled via the UIPart interface.
type UIMessage struct {
	ID       string          `json:"id"`
	Role     string          `json:"role"`
	Parts    []UIPart        `json:"-"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

func (m UIMessage) MarshalJSON() ([]byte, error) {
	parts := make([]json.RawMessage, len(m.Parts))
	for i, p := range m.Parts {
		data, err := MarshalUIPart(p)
		if err != nil {
			return nil, fmt.Errorf("marshal UIMessage.Parts[%d]: %w", i, err)
		}
		parts[i] = data
	}
	return json.Marshal(struct {
		ID       string            `json:"id"`
		Role     string            `json:"role"`
		Parts    []json.RawMessage `json:"parts"`
		Metadata json.RawMessage   `json:"metadata,omitempty"`
	}{m.ID, m.Role, parts, m.Metadata})
}

func (m *UIMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       string            `json:"id"`
		Role     string            `json:"role"`
		Parts    []json.RawMessage `json:"parts"`
		Metadata json.RawMessage   `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.ID = raw.ID
	m.Role = raw.Role
	m.Metadata = raw.Metadata
	m.Parts = make([]UIPart, 0, len(raw.Parts))
	for _, partData := range raw.Parts {
		p, err := UnmarshalUIPart(partData)
		if err != nil {
			continue // skip unknown part types
		}
		m.Parts = append(m.Parts, p)
	}
	return nil
}

// ProjectUIMessages converts a slice of Messages (which may include "tool" role
// messages) into the AI SDK v6 UIMessage format. Consecutive assistant/tool
// runs are merged into single assistant UIMessages with DynamicToolParts.
func ProjectUIMessages(messages []Message) ([]UIMessage, error) {
	var result []UIMessage
	i := 0
	for i < len(messages) {
		msg := messages[i]
		if msg.Synthetic {
			i++
			continue
		}
		switch msg.Role {
		case "system":
			result = append(result, buildUISystemMessage(msg))
			i++
		case "user":
			result = append(result, buildUIUserMessage(msg))
			i++
		case "assistant":
			ui := UIMessage{
				ID:       msg.ID,
				Role:     "assistant",
				Metadata: msg.Metadata,
			}
			for i < len(messages) && (messages[i].Role == "assistant" || messages[i].Role == "tool") {
				if messages[i].Role != "assistant" {
					i++
					continue
				}
				ass := messages[i]
				i++
				var toolMsgs []Message
				for i < len(messages) && messages[i].Role == "tool" {
					toolMsgs = append(toolMsgs, messages[i])
					i++
				}
				if len(ui.Parts) > 0 {
					ui.Parts = append(ui.Parts, UIStepStartPart{Type: "step-start"})
				}

				parts, err := convertAssistantToolStepToUI(ass, toolMsgs)
				if err != nil {
					return nil, err
				}
				ui.Parts = append(ui.Parts, parts...)
			}
			result = append(result, ui)
		default:
			// Skip unknown roles (including orphan "tool" messages).
			i++
		}
	}
	return result, nil
}

func buildUISystemMessage(msg Message) UIMessage {
	text := ""
	for _, p := range msg.Parts {
		if tp, ok := p.(TextPart); ok {
			text += tp.Text
		}
	}
	return UIMessage{
		ID:       msg.ID,
		Role:     "system",
		Parts:    []UIPart{UITextPart{Type: "text", Text: text, State: "done"}},
		Metadata: msg.Metadata,
	}
}

func buildUIUserMessage(msg Message) UIMessage {
	ui := UIMessage{
		ID:       msg.ID,
		Role:     "user",
		Metadata: msg.Metadata,
	}
	for _, p := range msg.Parts {
		switch v := p.(type) {
		case TextPart:
			ui.Parts = append(ui.Parts, UITextPart{Type: "text", Text: v.Text, State: "done", ProviderMetadata: v.ProviderMetadata})
		case FilePart:
			ui.Parts = append(ui.Parts, UIFilePart{Type: "file", URL: v.Data, MediaType: v.MediaType, Filename: v.Filename, ProviderMetadata: v.ProviderMetadata})
		case ImagePart:
			ui.Parts = append(ui.Parts, UIFilePart{Type: "file", URL: v.Image, MediaType: v.MediaType})
		}
	}
	return ui
}

func convertAssistantToolStepToUI(ass Message, toolMsgs []Message) ([]UIPart, error) {
	toolResults := make(map[string]ToolResultPart)
	approvalResponses := make(map[string]ToolApprovalResponse)
	approvalResponsesByToolCall := make(map[string]ToolApprovalResponse)
	approvalRequests := make(map[string]ToolApprovalRequest)

	for _, p := range ass.Parts {
		if ar, ok := p.(ToolApprovalRequest); ok {
			approvalRequests[ar.ToolCallID] = ar
		}
	}
	for _, toolMsg := range toolMsgs {
		for _, p := range toolMsg.Parts {
			switch v := p.(type) {
			case ToolResultPart:
				toolResults[v.ToolCallID] = v
			case ToolApprovalResponse:
				approvalResponses[v.ApprovalID] = v
				if v.ToolCallID != "" {
					approvalResponsesByToolCall[v.ToolCallID] = v
				}
			case ToolApprovalRequest:
				approvalRequests[v.ToolCallID] = v
			}
		}
	}

	var parts []UIPart
	type dynEntry struct {
		idx int
		dp  DynamicToolPart
	}
	toolCallDyns := make(map[string]*dynEntry)

	for _, p := range ass.Parts {
		switch v := p.(type) {
		case TextPart:
			parts = append(parts, UITextPart{Type: "text", Text: v.Text, State: "done", ProviderMetadata: v.ProviderMetadata})

		case ReasoningPart:
			parts = append(parts, UIReasoningPart{Type: "reasoning", Text: v.Text, State: "done", ProviderMetadata: v.ProviderMetadata})

		case FilePart:
			parts = append(parts, UIFilePart{Type: "file", URL: v.Data, MediaType: v.MediaType, Filename: v.Filename, ProviderMetadata: v.ProviderMetadata})

		case ToolCallPart:
			dp := DynamicToolPart{
				Type:             "dynamic-tool",
				ToolName:         v.ToolName,
				ToolCallID:       v.ToolCallID,
				Input:            toolInputJSONValue(v.Input),
				ProviderExecuted: v.ProviderExecuted,
				State:            "input-available",
			}

			// Attach approval if present and derive state.
			if ar, ok := approvalRequests[v.ToolCallID]; ok {
				dp.Approval = &ToolApproval{ID: ar.ApprovalID}
				if resp, ok := approvalResponses[ar.ApprovalID]; ok {
					dp.Approval.Approved = &resp.Approved
					dp.Approval.Reason = resp.Reason
				} else {
					dp.State = "approval-requested"
				}
			} else if resp, ok := approvalResponsesByToolCall[v.ToolCallID]; ok {
				dp.Approval = &ToolApproval{ID: resp.ApprovalID, Approved: &resp.Approved, Reason: resp.Reason}
			}

			if result, ok := toolResults[v.ToolCallID]; ok {
				applyToolResultToDynamicPart(&dp, result)
			}

			idx := len(parts)
			parts = append(parts, dp)
			toolCallDyns[v.ToolCallID] = &dynEntry{idx: idx, dp: dp}

		case ToolResultPart:
			if entry, ok := toolCallDyns[v.ToolCallID]; ok {
				applyToolResultToDynamicPart(&entry.dp, v)
				parts[entry.idx] = entry.dp
			}

		case ToolApprovalRequest:
			// Already handled when processing ToolCallPart and tool event messages.

		case SourceURLPart:
			parts = append(parts, UISourceURLPart{Type: "source-url", SourceID: v.SourceID, URL: v.URL, Title: v.Title, ProviderMetadata: v.ProviderMetadata})

		case SourceDocumentPart:
			parts = append(parts, UISourceDocumentPart{Type: "source-document", SourceID: v.SourceID, MediaType: v.MediaType, Title: v.Title, Filename: v.Filename, ProviderMetadata: v.ProviderMetadata})

		case DataPart:
			parts = append(parts, UIDataPart{Type: "data-" + v.DataType, ID: v.ID, Data: v.Data})
		}
	}

	return parts, nil
}

func applyToolResultToDynamicPart(dp *DynamicToolPart, result ToolResultPart) {
	if result.Output == nil {
		return
	}
	switch o := result.Output.(type) {
	case JSONOutput:
		dp.State = "output-available"
		dp.Output = o.Value
	case TextOutput:
		dp.State = "output-available"
		data, _ := json.Marshal(o.Value)
		dp.Output = data
	case ErrorTextOutput:
		dp.State = "output-error"
		dp.ErrorText = o.Value
	case ErrorJSONOutput:
		dp.State = "output-error"
		dp.ErrorText = string(o.Value)
	case ExecutionDeniedOutput:
		dp.State = "output-denied"
	default:
		dp.State = "output-available"
		data, _ := MarshalToolResultOutput(result.Output)
		dp.Output = data
	}
}
