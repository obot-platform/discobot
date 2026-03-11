package sessionconfig

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/providers"
)

func TestBuiltinTools_AllDefined(t *testing.T) {
	tools := BuiltinTools("")

	expectedNames := []string{
		// Execution
		"Bash",
		// File operations
		"Read", "Write", "Edit", "apply_patch", "NotebookEdit",
		// Search
		"Glob", "Grep",
		// Web
		"WebFetch", "WebSearch",
		// Agent orchestration
		"Task",
		// Task management
		"TodoWrite",
		// Background tasks
		"TaskOutput", "TaskStop",
		// User interaction
		"AskUserQuestion",
		// Plan mode
		"EnterPlanMode", "ExitPlanMode",
		// Skills
		"Skill",
	}

	if len(tools) != len(expectedNames) {
		t.Fatalf("expected %d tools, got %d", len(expectedNames), len(tools))
	}

	nameSet := make(map[string]bool)
	for _, tool := range tools {
		nameSet[tool.Name] = true
	}

	for _, name := range expectedNames {
		if !nameSet[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestBuiltinTools_ValidSchemas(t *testing.T) {
	tools := BuiltinTools("")

	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}

		// Verify input schema is valid JSON.
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %s has invalid JSON schema: %v", tool.Name, err)
			continue
		}

		// Should be an object type with properties.
		if schema["type"] != "object" {
			t.Errorf("tool %s schema type = %v, want object", tool.Name, schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Errorf("tool %s schema missing properties", tool.Name)
		}
	}
}

func TestBuiltinTools_BashSchema(t *testing.T) {
	schema := findToolSchema(t, "Bash")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"command", "description", "timeout", "run_in_background"} {
		if _, ok := props[field]; !ok {
			t.Errorf("Bash schema missing '%s' property", field)
		}
	}

	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "command" {
		t.Errorf("Bash required = %v, want [command]", required)
	}
}

func TestBuiltinTools_ReadSchema(t *testing.T) {
	schema := findToolSchema(t, "Read")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"file_path", "offset", "limit", "pages"} {
		if _, ok := props[field]; !ok {
			t.Errorf("Read schema missing '%s' property", field)
		}
	}
}

func TestBuiltinTools_EditSchema(t *testing.T) {
	schema := findToolSchema(t, "Edit")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"file_path", "old_string", "new_string", "replace_all"} {
		if _, ok := props[field]; !ok {
			t.Errorf("Edit schema missing '%s' property", field)
		}
	}
}

func TestBuiltinTools_ApplyPatchSchema(t *testing.T) {
	schema := findToolSchema(t, "apply_patch")
	props := schema["properties"].(map[string]any)

	if _, ok := props["input"]; !ok {
		t.Error("apply_patch schema missing 'input' property")
	}

	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "input" {
		t.Errorf("apply_patch required = %v, want [input]", required)
	}
}

func TestBuiltinTools_ApplyPatchCustomFormat(t *testing.T) {
	tools := BuiltinTools("")
	var applyPatch *providers.ToolDefinition
	for i := range tools {
		if tools[i].Name == "apply_patch" {
			applyPatch = &tools[i]
			break
		}
	}
	if applyPatch == nil {
		t.Fatal("apply_patch tool not found")
	}
	if applyPatch.Type != "custom" {
		t.Fatalf("apply_patch type = %q, want custom", applyPatch.Type)
	}
	if applyPatch.Format == nil {
		t.Fatal("apply_patch format is nil")
	}
	if applyPatch.Format.Type != "grammar" {
		t.Fatalf("apply_patch format.type = %q, want grammar", applyPatch.Format.Type)
	}
	if applyPatch.Format.Syntax != "lark" {
		t.Fatalf("apply_patch format.syntax = %q, want lark", applyPatch.Format.Syntax)
	}
	if strings.TrimSpace(applyPatch.Format.Definition) == "" {
		t.Fatal("apply_patch format.definition is empty")
	}
}

func TestBuiltinTools_GrepSchema(t *testing.T) {
	schema := findToolSchema(t, "Grep")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{
		"pattern", "path", "glob", "type", "output_mode",
		"-i", "-n", "-A", "-B", "-C", "context",
		"multiline", "head_limit", "offset",
	} {
		if _, ok := props[field]; !ok {
			t.Errorf("Grep schema missing '%s' property", field)
		}
	}
}

func TestBuiltinTools_NotebookEditSchema(t *testing.T) {
	schema := findToolSchema(t, "NotebookEdit")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"notebook_path", "new_source", "cell_id", "cell_type", "edit_mode"} {
		if _, ok := props[field]; !ok {
			t.Errorf("NotebookEdit schema missing '%s' property", field)
		}
	}
}

func TestBuiltinTools_TaskSchema(t *testing.T) {
	schema := findToolSchema(t, "Task")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"description", "prompt", "subagent_type", "model", "resume", "run_in_background", "max_turns", "allowed_tools"} {
		if _, ok := props[field]; !ok {
			t.Errorf("Task schema missing '%s' property", field)
		}
	}

	required := schema["required"].([]any)
	reqSet := make(map[string]bool)
	for _, r := range required {
		reqSet[r.(string)] = true
	}
	for _, r := range []string{"description", "prompt", "subagent_type"} {
		if !reqSet[r] {
			t.Errorf("Task missing required field: %s", r)
		}
	}
}

func TestBuiltinTools_TodoWriteSchema(t *testing.T) {
	schema := findToolSchema(t, "TodoWrite")
	props := schema["properties"].(map[string]any)

	if _, ok := props["todos"]; !ok {
		t.Error("TodoWrite schema missing 'todos' property")
	}

	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "todos" {
		t.Errorf("TodoWrite required = %v, want [todos]", required)
	}
}

func TestBuiltinTools_AskUserQuestionSchema(t *testing.T) {
	schema := findToolSchema(t, "AskUserQuestion")
	props := schema["properties"].(map[string]any)

	if _, ok := props["questions"]; !ok {
		t.Error("AskUserQuestion schema missing 'questions' property")
	}
}

func TestBuiltinTools_WebSearchSchema(t *testing.T) {
	schema := findToolSchema(t, "WebSearch")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"query", "allowed_domains", "blocked_domains"} {
		if _, ok := props[field]; !ok {
			t.Errorf("WebSearch schema missing '%s' property", field)
		}
	}
}

func TestBuiltinTools_SkillSchema(t *testing.T) {
	schema := findToolSchema(t, "Skill")
	props := schema["properties"].(map[string]any)

	for _, field := range []string{"skill", "args"} {
		if _, ok := props[field]; !ok {
			t.Errorf("Skill schema missing '%s' property", field)
		}
	}

	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "skill" {
		t.Errorf("Skill required = %v, want [skill]", required)
	}
}

func TestBuiltinTools_WebSearchDescriptionUsesCurrentMonthYear(t *testing.T) {
	tools := BuiltinTools("")

	var description string
	for _, tool := range tools {
		if tool.Name == "WebSearch" {
			description = tool.Description
			break
		}
	}
	if description == "" {
		t.Fatal("WebSearch tool not found")
	}

	monthYear := time.Now().Format("January 2006")
	if !strings.Contains(description, "The current month is "+monthYear+".") {
		t.Errorf("WebSearch description should include current month/year %q", monthYear)
	}
}

// findToolSchema returns the parsed input schema for a tool by name.
func findToolSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	tools := BuiltinTools("")
	for _, tool := range tools {
		if tool.Name == name {
			var schema map[string]any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Fatalf("%s schema: %v", name, err)
			}
			return schema
		}
	}
	t.Fatalf("%s tool not found", name)
	return nil
}
