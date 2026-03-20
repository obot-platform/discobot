package tools

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/modelsdev"
)

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"` // 1-based line number to start reading from
	Limit    int    `json:"limit"`  // number of lines to read
	Pages    string `json:"pages"`
}

func (e *Executor) executeRead(toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input readInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if input.FilePath == "" {
		return errResult(call, "file_path is required"), nil
	}

	// Resolve path: absolute paths are allowed for the agent.
	path := resolvePath(e.cwd, input.FilePath)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(call, fmt.Sprintf("file not found: %s", input.FilePath)), nil
		}
		return errResult(call, err.Error()), nil
	}
	if info.IsDir() {
		return errResult(call, fmt.Sprintf("%s is a directory", input.FilePath)), nil
	}

	const maxBytes = 10 * 1024 * 1024 // 10 MB
	if info.Size() > maxBytes {
		return errResult(call, fmt.Sprintf("file too large (%d bytes, max %d)", info.Size(), maxBytes)), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	// Record the mtime+size so Write/Edit know the file was read.
	e.recordFileRead(path, info)

	mediaType := detectReadMediaType(path, data)
	if strings.HasPrefix(mediaType, "image/") {
		if e.modelSupportsInputModality(toolCtx, "image") {
			return contentResult(call,
				message.ContentTextItem{
					Text: fmt.Sprintf("Read image file %q (%s, %d bytes). Included image data for multimodal inspection.", input.FilePath, mediaType, info.Size()),
				},
				message.ContentImageDataItem{
					Data:      base64.StdEncoding.EncodeToString(data),
					MediaType: mediaType,
				},
			), nil
		}
		return textResult(call, fmt.Sprintf("Read image file %q (%s, %d bytes). Current model %s does not advertise image input support, so returning metadata only.", input.FilePath, mediaType, info.Size(), toolContextModelRef(toolCtx))), nil
	}

	if mediaType == "application/pdf" {
		pageSelection, err := parsePDFPageSelection(input.Pages)
		if err != nil {
			return errResult(call, err.Error()), nil
		}

		approxPages := estimatePDFPageCount(data)
		if approxPages > 10 && pageSelection == nil {
			return errResult(call, fmt.Sprintf("PDF file %q appears to have %d pages. For PDFs over 10 pages, pages is required (for example: \"1-5\").", input.FilePath, approxPages)), nil
		}
		if pageSelection != nil && approxPages > 0 && pageSelection.End > approxPages {
			return errResult(call, fmt.Sprintf("pages range %q exceeds detected PDF page count (%d)", input.Pages, approxPages)), nil
		}

		pageDetails := ""
		if pageSelection != nil {
			if pageSelection.Start == pageSelection.End {
				pageDetails = fmt.Sprintf(" Requested pages: %d.", pageSelection.Start)
			} else {
				pageDetails = fmt.Sprintf(" Requested pages: %d-%d.", pageSelection.Start, pageSelection.End)
			}
		}
		pageCountDetails := ""
		if approxPages > 0 {
			pageCountDetails = fmt.Sprintf(" (detected %d pages)", approxPages)
		}

		supportsPDF := e.modelSupportsInputModality(toolCtx, "pdf")
		supportsImage := e.modelSupportsInputModality(toolCtx, "image")

		if supportsPDF {
			return contentResult(call,
				message.ContentTextItem{
					Text: fmt.Sprintf("Read PDF file %q (%d bytes)%s. Included PDF data for multimodal inspection.%s", input.FilePath, info.Size(), pageCountDetails, pageDetails),
				},
				message.ContentFileDataItem{
					Data:      base64.StdEncoding.EncodeToString(data),
					MediaType: "application/pdf",
					Filename:  filepath.Base(path),
				},
			), nil
		}

		if !supportsPDF && supportsImage {
			renderedImages, rendererName, renderErr := renderPDFPagesToImages(path, pageSelection, approxPages)
			if renderErr == nil && len(renderedImages) > 0 {
				items := make([]message.ToolResultContentItem, 0, len(renderedImages)+1)
				summaryText := fmt.Sprintf("Read PDF file %q (%d bytes)%s. Current model %s does not advertise PDF input support, so converted pages to images via %s for multimodal inspection.%s", input.FilePath, info.Size(), pageCountDetails, toolContextModelRef(toolCtx), rendererName, pageDetails)
				items = append(items, message.ContentTextItem{Text: summaryText})
				for _, item := range renderedImages {
					items = append(items, item)
				}
				return contentResult(call, items...), nil
			}
			if renderErr != nil {
				return textResult(call, fmt.Sprintf("Read PDF file %q (%d bytes)%s. Current model %s does not advertise PDF input support, and PDF-to-image CLI conversion failed (%v), so returning metadata only.%s", input.FilePath, info.Size(), pageCountDetails, toolContextModelRef(toolCtx), renderErr, pageDetails)), nil
			}
		}

		return textResult(call, fmt.Sprintf("Read PDF file %q (%d bytes)%s. Current model %s does not advertise PDF input support, so returning metadata only.%s", input.FilePath, info.Size(), pageCountDetails, toolContextModelRef(toolCtx), pageDetails)), nil
	}

	content := string(data)

	// Apply line offset and limit if requested.
	if input.Offset > 0 || input.Limit > 0 {
		content = sliceLines(content, input.Offset, input.Limit)
	}

	// Format output with line numbers (matching Claude Code's cat -n style).
	output := addLineNumbers(content, maxOf(input.Offset, 1))

	return textResult(call, output), nil
}

func contentResult(call message.ToolCallPart, items ...message.ToolResultContentItem) thread.ToolExecuteResult {
	return thread.ToolExecuteResult{
		Result: message.ToolResultPart{
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Output: message.ContentOutput{
				Value: items,
			},
		},
	}
}

func (e *Executor) modelSupportsInputModality(toolCtx *thread.ToolContext, modality string) bool {
	if toolCtx == nil || toolCtx.ProviderID == "" || toolCtx.ModelID == "" {
		return false
	}
	model := modelsdev.Lookup(toolCtx.ProviderID, toolCtx.ModelID)
	return model != nil && model.SupportsInputModality(modality)
}

func toolContextModelRef(toolCtx *thread.ToolContext) string {
	if toolCtx == nil || toolCtx.ProviderID == "" || toolCtx.ModelID == "" {
		return "(unknown model)"
	}
	return toolCtx.ProviderID + "/" + toolCtx.ModelID
}

func detectReadMediaType(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".pdf" {
		return "application/pdf"
	}
	if mediaType, ok := mediaTypeByExtension(ext); ok {
		return mediaType
	}
	if len(data) == 0 {
		return "application/octet-stream"
	}
	sample := data
	if len(sample) > 512 {
		sample = sample[:512]
	}
	mediaType := strings.ToLower(http.DetectContentType(sample))
	if i := strings.Index(mediaType, ";"); i >= 0 {
		mediaType = mediaType[:i]
	}
	if mediaType == "application/octet-stream" {
		if extMediaType, ok := mediaTypeByExtension(ext); ok {
			return extMediaType
		}
	}
	return mediaType
}

func mediaTypeByExtension(ext string) (string, bool) {
	switch ext {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".bmp":
		return "image/bmp", true
	case ".svg":
		return "image/svg+xml", true
	case ".ico":
		return "image/x-icon", true
	case ".tif", ".tiff":
		return "image/tiff", true
	case ".avif":
		return "image/avif", true
	case ".heic":
		return "image/heic", true
	case ".heif":
		return "image/heif", true
	case ".pdf":
		return "application/pdf", true
	default:
		return "", false
	}
}

type pdfPageSelection struct {
	Start int
	End   int
}

func parsePDFPageSelection(raw string) (*pdfPageSelection, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, "-")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid pages value %q: expected \"N\" or \"N-M\"", raw)
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || start < 1 {
		return nil, fmt.Errorf("invalid pages value %q: start page must be a positive integer", raw)
	}

	end := start
	if len(parts) == 2 {
		end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || end < 1 {
			return nil, fmt.Errorf("invalid pages value %q: end page must be a positive integer", raw)
		}
	}

	if end < start {
		return nil, fmt.Errorf("invalid pages value %q: end page must be greater than or equal to start page", raw)
	}
	if end-start+1 > 20 {
		return nil, fmt.Errorf("invalid pages value %q: range must include at most 20 pages", raw)
	}

	return &pdfPageSelection{Start: start, End: end}, nil
}

func estimatePDFPageCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	pages := bytes.Count(data, []byte("/Type /Page"))
	pageTrees := bytes.Count(data, []byte("/Type /Pages"))
	count := pages - pageTrees
	if count < 0 {
		return 0
	}
	return count
}

func renderPDFPagesToImages(pdfPath string, selection *pdfPageSelection, approxPages int) ([]message.ContentImageDataItem, string, error) {
	start, end := resolvePDFRenderRange(selection, approxPages)
	tmpDir, err := os.MkdirTemp("", "discobot-read-pdf-images-*")
	if err != nil {
		return nil, "", fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var errors []string
	foundRenderer := false

	if bin, err := exec.LookPath("pdftoppm"); err == nil {
		foundRenderer = true
		prefix := filepath.Join(tmpDir, "page")
		args := []string{"-png", "-f", strconv.Itoa(start), "-l", strconv.Itoa(end), pdfPath, prefix}
		items, renderErr := runPDFRenderCommand(bin, args, prefix+"-*.png")
		if renderErr == nil {
			return items, "pdftoppm", nil
		}
		errors = append(errors, "pdftoppm: "+renderErr.Error())
	}

	if bin, err := exec.LookPath("pdftocairo"); err == nil {
		foundRenderer = true
		prefix := filepath.Join(tmpDir, "page")
		args := []string{"-png", "-f", strconv.Itoa(start), "-l", strconv.Itoa(end), pdfPath, prefix}
		items, renderErr := runPDFRenderCommand(bin, args, prefix+"-*.png")
		if renderErr == nil {
			return items, "pdftocairo", nil
		}
		errors = append(errors, "pdftocairo: "+renderErr.Error())
	}

	if bin, err := exec.LookPath("mutool"); err == nil {
		foundRenderer = true
		pageSpec := strconv.Itoa(start)
		if end > start {
			pageSpec = fmt.Sprintf("%d-%d", start, end)
		}
		outputPattern := filepath.Join(tmpDir, "page-*.png")
		args := []string{"draw", "-q", "-F", "png", "-o", filepath.Join(tmpDir, "page-%d.png"), pdfPath, pageSpec}
		items, renderErr := runPDFRenderCommand(bin, args, outputPattern)
		if renderErr == nil {
			return items, "mutool", nil
		}
		errors = append(errors, "mutool: "+renderErr.Error())
	}

	if !foundRenderer {
		return nil, "", fmt.Errorf("no supported PDF-to-image CLI found (tried pdftoppm, pdftocairo, mutool)")
	}
	return nil, "", fmt.Errorf("all available PDF-to-image CLIs failed (%s)", strings.Join(errors, "; "))
}

func resolvePDFRenderRange(selection *pdfPageSelection, approxPages int) (int, int) {
	if selection != nil {
		return selection.Start, selection.End
	}
	if approxPages > 0 {
		return 1, approxPages
	}
	return 1, 1
}

func runPDFRenderCommand(binary string, args []string, outputPattern string) ([]message.ContentImageDataItem, error) {
	cmd := exec.Command(binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		details := strings.TrimSpace(string(out))
		if details != "" {
			return nil, fmt.Errorf("%w: %s", err, details)
		}
		return nil, err
	}

	paths, err := filepath.Glob(outputPattern)
	if err != nil {
		return nil, fmt.Errorf("glob rendered files: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("renderer produced no image files")
	}

	sort.Slice(paths, func(i, j int) bool {
		iPage, iOK := trailingNumberBeforeExtension(paths[i])
		jPage, jOK := trailingNumberBeforeExtension(paths[j])
		if iOK && jOK && iPage != jPage {
			return iPage < jPage
		}
		return paths[i] < paths[j]
	})

	items := make([]message.ContentImageDataItem, 0, len(paths))
	for _, imgPath := range paths {
		data, err := os.ReadFile(imgPath)
		if err != nil {
			return nil, fmt.Errorf("read rendered image %q: %w", imgPath, err)
		}
		mediaType := detectReadMediaType(imgPath, data)
		if !strings.HasPrefix(mediaType, "image/") {
			continue
		}
		items = append(items, message.ContentImageDataItem{
			Data:      base64.StdEncoding.EncodeToString(data),
			MediaType: mediaType,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("renderer produced files but no readable image content")
	}
	return items, nil
}

func trailingNumberBeforeExtension(path string) (int, bool) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	i := len(name) - 1
	for i >= 0 && name[i] >= '0' && name[i] <= '9' {
		i--
	}
	if i == len(name)-1 {
		return 0, false
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return 0, false
	}
	return n, true
}

// sliceLines extracts lines [offset, offset+limit) from content.
// offset is 1-based; 0 means start from beginning.
func sliceLines(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")

	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start >= len(lines) {
		return ""
	}

	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	return strings.Join(lines[start:end], "\n")
}

// addLineNumbers prefixes each line with its line number (cat -n style).
// startLine is the 1-based line number of the first line.
func addLineNumbers(content string, startLine int) string {
	if startLine < 1 {
		startLine = 1
	}
	lines := strings.Split(content, "\n")
	// Remove trailing empty line from final newline split.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var sb strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&sb, "%6d→%s\n", startLine+i, line)
	}
	return sb.String()
}

func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}
