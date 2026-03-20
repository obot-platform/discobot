package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/modelsdev"
)

func executeRead(t *testing.T, e *Executor, toolCtx *thread.ToolContext, input map[string]any) message.ToolResultOutput {
	t.Helper()
	output := executeReadRaw(t, e, toolCtx, input)
	if errOut, ok := output.(message.ErrorTextOutput); ok {
		t.Fatalf("unexpected Read error output: %s", errOut.Value)
	}
	return output
}

func executeReadRaw(t *testing.T, e *Executor, toolCtx *thread.ToolContext, input map[string]any) message.ToolResultOutput {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	result, err := e.Execute(context.Background(), toolCtx, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   "Read",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	return result.Result.Output
}

func buildPDFWithPageCount(pageCount int) []byte {
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	b.WriteString("2 0 obj\n<< /Type /Pages /Count ")
	b.WriteString(fmt.Sprintf("%d", pageCount))
	b.WriteString(" /Kids [")
	for i := 0; i < pageCount; i++ {
		b.WriteString(fmt.Sprintf("%d 0 R ", i+3))
	}
	b.WriteString("] >>\nendobj\n")
	for i := 0; i < pageCount; i++ {
		objNum := i + 3
		b.WriteString(fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n", objNum))
	}
	b.WriteString("%%EOF\n")
	return []byte(b.String())
}

func findImageOnlyModelContext() *thread.ToolContext {
	candidates := []thread.ToolContext{
		{ProviderID: "openai", ModelID: "gpt-4o"},
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
		{ProviderID: "openai", ModelID: "gpt-4.1"},
		{ProviderID: "google", ModelID: "gemini-2.0-flash-001"},
		{ProviderID: "google", ModelID: "gemini-2.5-flash"},
	}
	for _, candidate := range candidates {
		model := modelsdev.Lookup(candidate.ProviderID, candidate.ModelID)
		if model == nil {
			continue
		}
		if model.SupportsInputModality("image") && !model.SupportsInputModality("pdf") {
			ctx := candidate
			return &ctx
		}
	}
	return nil
}

func TestRead_ImageWithSupportedModelReturnsMultimodalContent(t *testing.T) {
	cwd := t.TempDir()
	imagePath := filepath.Join(cwd, "sample.png")
	// 1x1 transparent PNG.
	pngData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAF/wJ/lrLZ2QAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode png fixture: %v", err)
	}
	if err := os.WriteFile(imagePath, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, &thread.ToolContext{ProviderID: "openai", ModelID: "gpt-4o"}, map[string]any{
		"file_path": imagePath,
	})

	content, ok := output.(message.ContentOutput)
	if !ok {
		t.Fatalf("expected ContentOutput, got %T", output)
	}
	if len(content.Value) < 2 {
		t.Fatalf("expected at least 2 content items, got %d", len(content.Value))
	}

	textItem, ok := content.Value[0].(message.ContentTextItem)
	if !ok {
		t.Fatalf("expected first content item to be text, got %T", content.Value[0])
	}
	if !strings.Contains(textItem.Text, "Read image file") {
		t.Errorf("expected image summary text, got %q", textItem.Text)
	}

	var imageItem message.ContentImageDataItem
	foundImage := false
	for _, item := range content.Value {
		if candidate, ok := item.(message.ContentImageDataItem); ok {
			imageItem = candidate
			foundImage = true
			break
		}
	}
	if !foundImage {
		t.Fatalf("expected image-data item in content output")
	}
	if imageItem.MediaType != "image/png" {
		t.Errorf("expected media type image/png, got %q", imageItem.MediaType)
	}
	expectedBase64 := base64.StdEncoding.EncodeToString(pngData)
	if imageItem.Data != expectedBase64 {
		t.Errorf("expected base64 image data to match file contents")
	}
}

func TestRead_ImageWithoutSupportedModelReturnsTextSummary(t *testing.T) {
	cwd := t.TempDir()
	imagePath := filepath.Join(cwd, "sample.png")
	pngData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAF/wJ/lrLZ2QAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode png fixture: %v", err)
	}
	if err := os.WriteFile(imagePath, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, &thread.ToolContext{ProviderID: "302ai", ModelID: "MiniMax-M1"}, map[string]any{
		"file_path": imagePath,
	})

	text, ok := output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput fallback, got %T", output)
	}
	if !strings.Contains(text.Value, "does not advertise image input support") {
		t.Errorf("expected unsupported-image fallback message, got %q", text.Value)
	}
}

func TestRead_PDFWithSupportedModelReturnsMultimodalContent(t *testing.T) {
	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "sample.pdf")
	pdfData := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, &thread.ToolContext{ProviderID: "anthropic", ModelID: "claude-3-7-sonnet-20250219"}, map[string]any{
		"file_path": pdfPath,
	})

	content, ok := output.(message.ContentOutput)
	if !ok {
		t.Fatalf("expected ContentOutput, got %T", output)
	}

	var fileItem message.ContentFileDataItem
	foundFile := false
	for _, item := range content.Value {
		if candidate, ok := item.(message.ContentFileDataItem); ok {
			fileItem = candidate
			foundFile = true
			break
		}
	}
	if !foundFile {
		t.Fatalf("expected file-data item in PDF content output")
	}
	if fileItem.MediaType != "application/pdf" {
		t.Errorf("expected media type application/pdf, got %q", fileItem.MediaType)
	}
	if fileItem.Filename != "sample.pdf" {
		t.Errorf("expected filename sample.pdf, got %q", fileItem.Filename)
	}
	expectedBase64 := base64.StdEncoding.EncodeToString(pdfData)
	if fileItem.Data != expectedBase64 {
		t.Errorf("expected base64 PDF data to match file contents")
	}
}

func TestRead_PDFWithoutSupportedModelReturnsTextSummary(t *testing.T) {
	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "sample.pdf")
	pdfData := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n%%EOF\n")
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, &thread.ToolContext{ProviderID: "302ai", ModelID: "MiniMax-M1"}, map[string]any{
		"file_path": pdfPath,
	})

	text, ok := output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput fallback, got %T", output)
	}
	if !strings.Contains(text.Value, "does not advertise PDF input support") {
		t.Errorf("expected unsupported-pdf fallback message, got %q", text.Value)
	}
}

func TestRead_PDFLargeWithoutPagesReturnsError(t *testing.T) {
	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "large.pdf")
	pdfData := buildPDFWithPageCount(12)
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeReadRaw(t, e, &thread.ToolContext{ProviderID: "anthropic", ModelID: "claude-3-7-sonnet-20250219"}, map[string]any{
		"file_path": pdfPath,
	})

	errOut, ok := output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("expected ErrorTextOutput, got %T", output)
	}
	if !strings.Contains(errOut.Value, "pages is required") {
		t.Errorf("expected pages-required error, got %q", errOut.Value)
	}
}

func TestRead_PDFLargeWithTooManyRequestedPagesReturnsError(t *testing.T) {
	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "large.pdf")
	pdfData := buildPDFWithPageCount(30)
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeReadRaw(t, e, &thread.ToolContext{ProviderID: "anthropic", ModelID: "claude-3-7-sonnet-20250219"}, map[string]any{
		"file_path": pdfPath,
		"pages":     "1-25",
	})

	errOut, ok := output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("expected ErrorTextOutput, got %T", output)
	}
	if !strings.Contains(errOut.Value, "at most 20 pages") {
		t.Errorf("expected pages-limit error, got %q", errOut.Value)
	}
}

func TestRead_PDFLargeWithPagesReturnsMultimodalContent(t *testing.T) {
	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "large.pdf")
	pdfData := buildPDFWithPageCount(12)
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, &thread.ToolContext{ProviderID: "anthropic", ModelID: "claude-3-7-sonnet-20250219"}, map[string]any{
		"file_path": pdfPath,
		"pages":     "2-5",
	})

	content, ok := output.(message.ContentOutput)
	if !ok {
		t.Fatalf("expected ContentOutput, got %T", output)
	}
	if len(content.Value) == 0 {
		t.Fatalf("expected content items")
	}
	textItem, ok := content.Value[0].(message.ContentTextItem)
	if !ok {
		t.Fatalf("expected first content item to be text, got %T", content.Value[0])
	}
	if !strings.Contains(textItem.Text, "Requested pages: 2-5") {
		t.Errorf("expected requested-pages note in content text, got %q", textItem.Text)
	}
}

func TestRead_PDFWithImageOnlyModelAndCLIProducesImageContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on POSIX shell script")
	}
	toolCtx := findImageOnlyModelContext()
	if toolCtx == nil {
		t.Skip("no image-only model found in current models metadata")
	}

	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "sample.pdf")
	pdfData := buildPDFWithPageCount(1)
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	pngData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAF/wJ/lrLZ2QAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode png fixture: %v", err)
	}
	pngFixturePath := filepath.Join(cwd, "fixture.png")
	if err := os.WriteFile(pngFixturePath, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	scriptPath := filepath.Join(binDir, "pdftoppm")
	script := "#!/bin/sh\nset -eu\nfor last; do :; done\n/bin/cp \"$TEST_PNG_PATH\" \"${last}-1.png\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_PNG_PATH", pngFixturePath)
	t.Setenv("PATH", binDir)

	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, toolCtx, map[string]any{
		"file_path": pdfPath,
	})

	content, ok := output.(message.ContentOutput)
	if !ok {
		t.Fatalf("expected ContentOutput, got %T", output)
	}

	foundImage := false
	for _, item := range content.Value {
		if _, ok := item.(message.ContentImageDataItem); ok {
			foundImage = true
			break
		}
	}
	if !foundImage {
		t.Fatalf("expected rendered image content item in output")
	}
}

func TestRead_PDFWithImageOnlyModelWithoutCLIProducesTextFallback(t *testing.T) {
	toolCtx := findImageOnlyModelContext()
	if toolCtx == nil {
		t.Skip("no image-only model found in current models metadata")
	}

	cwd := t.TempDir()
	pdfPath := filepath.Join(cwd, "sample.pdf")
	pdfData := buildPDFWithPageCount(1)
	if err := os.WriteFile(pdfPath, pdfData, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", t.TempDir())
	e := New(cwd, t.TempDir(), t.Name())
	output := executeRead(t, e, toolCtx, map[string]any{
		"file_path": pdfPath,
	})

	text, ok := output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput fallback, got %T", output)
	}
	if !strings.Contains(text.Value, "no supported PDF-to-image CLI found") {
		t.Errorf("expected missing-cli fallback message, got %q", text.Value)
	}
}
