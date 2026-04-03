// Package services provides service discovery, process management, and proxying.
// Services are executable scripts in .discobot/services/ with YAML front matter.
package services

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// ServicesDir is the directory within the workspace where services are defined.
const ServicesDir = ".discobot/services"

// ServiceInfo represents a discovered service definition with runtime state.
type ServiceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Order       *int   `json:"order,omitempty"`
	HTTP        int    `json:"http,omitempty"`
	HTTPS       int    `json:"https,omitempty"`
	Path        string `json:"path"`
	URLPath     string `json:"urlPath,omitempty"`
	Status      string `json:"status"` // "running", "stopped", "starting", "stopping"
	Passive     bool   `json:"passive,omitempty"`
	PID         int    `json:"pid,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	ExitCode    *int   `json:"exitCode,omitempty"`
}

// Port returns the service's HTTP or HTTPS port, preferring HTTP.
func (s *ServiceInfo) Port() int {
	if s.HTTP > 0 {
		return s.HTTP
	}
	return s.HTTPS
}

// serviceConfig is the raw front matter config parsed from a service file.
type serviceConfig struct {
	Name        string
	Description string
	Order       int
	HasOrder    bool
	HTTP        int
	HTTPS       int
	URLPath     string
}

// Script extensions to strip when normalizing IDs.
var svcScriptExtensions = []string{
	".sh", ".bash", ".zsh", ".py", ".js", ".ts", ".rb", ".pl", ".php",
}

var svcNonAlphanumericRe = regexp.MustCompile(`[^a-z0-9_-]`)
var svcLeadingTrailingHyphens = regexp.MustCompile(`^-+|-+$`)

// normalizeServiceID converts a filename to a service ID.
func normalizeServiceID(filename string) string {
	id := filename
	lower := strings.ToLower(filename)
	for _, ext := range svcScriptExtensions {
		if strings.HasSuffix(lower, ext) {
			id = id[:len(id)-len(ext)]
			break
		}
	}
	id = strings.ReplaceAll(id, ".", "-")
	id = strings.ToLower(id)
	id = svcNonAlphanumericRe.ReplaceAllString(id, "")
	id = svcLeadingTrailingHyphens.ReplaceAllString(id, "")
	return id
}

// delimiterStyle describes the front matter delimiter format.
type svcDelimiterStyle struct {
	prefix string
}

// detectSvcDelimiter checks if a line is a front matter delimiter.
func detectSvcDelimiter(line string) *svcDelimiterStyle {
	trimmed := strings.TrimSpace(line)
	switch trimmed {
	case "---":
		return &svcDelimiterStyle{prefix: ""}
	case "#---":
		return &svcDelimiterStyle{prefix: "#"}
	case "//---":
		return &svcDelimiterStyle{prefix: "//"}
	}
	return nil
}

// stripSvcPrefix removes the comment prefix and leading whitespace from a content line.
func stripSvcPrefix(line, prefix string) string {
	if prefix == "" {
		return line
	}
	idx := strings.Index(line, prefix)
	if idx == -1 {
		return line
	}
	return strings.TrimLeft(line[idx+len(prefix):], " \t")
}

// parseServiceFrontMatter parses service YAML front matter from file content.
// Returns the config, the line number where the body starts, whether the file
// has a shebang, and whether the body is empty.
func parseServiceFrontMatter(content string) (cfg serviceConfig, bodyStart int, hasShebang bool, hasEmptyBody bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return cfg, 0, false, true
	}

	hasShebang = strings.HasPrefix(lines[0], "#!")
	startLine := 0
	if hasShebang {
		startLine = 1
	}

	if len(lines) <= startLine {
		return cfg, len(lines), hasShebang, true
	}

	delim := detectSvcDelimiter(lines[startLine])
	if delim == nil {
		bs := startLine
		if hasShebang {
			bs = 1
		}
		return cfg, bs, hasShebang, isBodyEmpty(lines, bs)
	}

	// Find closing delimiter
	var yamlLines []string
	closingLine := -1
	for i := startLine + 1; i < len(lines); i++ {
		if detectSvcDelimiter(lines[i]) != nil {
			closingLine = i
			break
		}
		// For comment styles, check that lines have the prefix or are blank
		if delim.prefix != "" {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed != "" && !strings.Contains(lines[i], delim.prefix) {
				// No prefix found on non-blank line — abort
				bs := startLine
				if hasShebang {
					bs = 1
				}
				return cfg, bs, hasShebang, isBodyEmpty(lines, bs)
			}
		}
		yamlLines = append(yamlLines, stripSvcPrefix(lines[i], delim.prefix))
	}

	if closingLine == -1 {
		bs := startLine
		if hasShebang {
			bs = 1
		}
		return cfg, bs, hasShebang, isBodyEmpty(lines, bs)
	}

	bodyStart = closingLine + 1
	hasEmptyBody = isBodyEmpty(lines, bodyStart)

	// Parse simple YAML key-value pairs
	for _, line := range yamlLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			continue
		}
		key := strings.TrimSpace(trimmed[:colonIdx])
		value := strings.TrimSpace(trimmed[colonIdx+1:])

		// Remove quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		switch key {
		case "name":
			cfg.Name = value
		case "description":
			cfg.Description = value
		case "order":
			if order, err := strconv.Atoi(value); err == nil {
				cfg.Order = order
				cfg.HasOrder = true
			}
		case "http":
			if port, err := strconv.Atoi(value); err == nil && port > 0 && port < 65536 {
				cfg.HTTP = port
			}
		case "https":
			if port, err := strconv.Atoi(value); err == nil && port > 0 && port < 65536 {
				cfg.HTTPS = port
			}
		case "path":
			if !strings.HasPrefix(value, "/") {
				value = "/" + value
			}
			cfg.URLPath = value
		}
	}

	return cfg, bodyStart, hasShebang, hasEmptyBody
}

// isBodyEmpty checks if all lines from bodyStart onward are whitespace.
func isBodyEmpty(lines []string, bodyStart int) bool {
	for i := bodyStart; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return false
		}
	}
	return true
}

// DiscoverServices finds all valid services in the given services directory.
func DiscoverServices(servicesDir string) ([]ServiceInfo, error) {
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var services []ServiceInfo
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		filePath := filepath.Join(servicesDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		cfg, _, hasShebang, hasEmptyBody := parseServiceFrontMatter(string(content))

		// Determine if passive
		isPassive := (cfg.HTTP > 0 || cfg.HTTPS > 0) && hasEmptyBody

		if !isPassive {
			// Non-passive: must be executable and have shebang. Windows has no
			// Unix-style execute bits, so rely on the shebang there.
			if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}
			if !hasShebang {
				continue
			}
		}

		id := normalizeServiceID(entry.Name())
		name := cfg.Name
		if name == "" {
			name = id
		}

		var order *int
		if cfg.HasOrder {
			orderValue := cfg.Order
			order = &orderValue
		}

		svc := ServiceInfo{
			ID:          id,
			Name:        name,
			Description: cfg.Description,
			Order:       order,
			HTTP:        cfg.HTTP,
			HTTPS:       cfg.HTTPS,
			Path:        filePath,
			URLPath:     cfg.URLPath,
			Status:      "stopped",
			Passive:     isPassive,
		}

		services = append(services, svc)
	}

	sort.Slice(services, func(i, j int) bool {
		leftOrder, rightOrder := services[i].Order, services[j].Order
		switch {
		case leftOrder != nil && rightOrder == nil:
			return true
		case leftOrder == nil && rightOrder != nil:
			return false
		case leftOrder != nil && rightOrder != nil && *leftOrder != *rightOrder:
			return *leftOrder < *rightOrder
		}

		if services[i].Name != services[j].Name {
			return services[i].Name < services[j].Name
		}

		if services[i].ID != services[j].ID {
			return services[i].ID < services[j].ID
		}

		return false
	})

	return services, nil
}
