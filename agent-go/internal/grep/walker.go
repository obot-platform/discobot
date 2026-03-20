package gogrep

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/charlievieth/fastwalk"

	"github.com/obot-platform/discobot/agent-go/internal/grep/filetypes"
	"github.com/obot-platform/discobot/agent-go/internal/grep/internal/gitignore"
)

func walk(ctx context.Context, opts GrepOptions, s *searcher) (*Results, error) {
	info, err := os.Stat(opts.Path)
	if err != nil {
		return nil, err
	}

	// Single file
	if !info.IsDir() {
		fm, err := s.searchFile(opts.Path, info.Size())
		if err != nil {
			return nil, err
		}
		r := &Results{}
		if fm != nil {
			r.Files = append(r.Files, *fm)
			r.TotalCount = fm.Count
		}
		return applyOffsetLimit(r, opts), nil
	}

	// Directory: parallel walk
	numWorkers := max(runtime.NumCPU(), 1)

	// Set up gitignore matcher
	var gi *gitignore.Matcher
	if shouldRespectGitignore(opts) {
		gi = gitignore.New(opts.Path)
	}

	type fileEntry struct {
		path string
		size int64
	}
	fileCh := make(chan fileEntry, numWorkers*64)
	resultCh := make(chan *FileMatches, numWorkers*64)
	var truncated atomic.Bool

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Go(func() {
			for fe := range fileCh {
				if ctx.Err() != nil || truncated.Load() {
					return
				}
				fm, err := s.searchFile(fe.path, fe.size)
				if err != nil || fm == nil {
					continue
				}
				resultCh <- fm
			}
		})
	}

	// Collector
	var results Results
	var collectorWg sync.WaitGroup
	collectorWg.Go(func() {
		for fm := range resultCh {
			results.Files = append(results.Files, *fm)
			results.TotalCount += fm.Count
		}
	})

	// Walk directory using fastwalk for parallel directory enumeration
	conf := &fastwalk.Config{
		NumWorkers: numWorkers,
	}
	_ = fastwalk.Walk(conf, opts.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil || truncated.Load() {
			return fastwalk.ErrSkipFiles
		}

		if d.IsDir() {
			name := d.Name()
			// Always skip .git directory
			if name == ".git" {
				return filepath.SkipDir
			}
			// Skip other hidden dirs
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			if gi != nil {
				// When gitignore is active, rely on it for directory filtering
				if gi.IsIgnored(path, true) {
					return filepath.SkipDir
				}
				// Load nested .gitignore in this directory
				gi.LoadDir(path)
			} else {
				// When no gitignore, apply default skip list
				switch name {
				case "node_modules", "__pycache__":
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check gitignore for files
		if gi != nil && gi.IsIgnored(path, false) {
			return nil
		}

		// Apply file type filter
		if opts.Type != "" && !filetypes.MatchesType(path, opts.Type) {
			return nil
		}

		// Apply glob filter
		if opts.Glob != "" {
			rel, relErr := filepath.Rel(opts.Path, path)
			if relErr != nil || !matchGlob(opts.Glob, rel) {
				return nil
			}
		}

		var size int64
		if info, err := d.Info(); err == nil {
			size = info.Size()
		}
		fileCh <- fileEntry{path: path, size: size}
		return nil
	})

	close(fileCh)
	wg.Wait()
	close(resultCh)
	collectorWg.Wait()

	return applyOffsetLimit(&results, opts), nil
}

func applyOffsetLimit(r *Results, opts GrepOptions) *Results {
	if opts.OutputMode == "content" {
		// Flatten all matches, apply offset/limit
		var allMatches []Match
		for _, fm := range r.Files {
			allMatches = append(allMatches, fm.Matches...)
		}

		if opts.Offset > 0 {
			if opts.Offset >= len(allMatches) {
				allMatches = nil
			} else {
				allMatches = allMatches[opts.Offset:]
			}
		}
		if opts.HeadLimit > 0 && len(allMatches) > opts.HeadLimit {
			allMatches = allMatches[:opts.HeadLimit]
			r.Truncated = true
		}

		// Rebuild file groups from filtered matches
		fileMap := make(map[string]*FileMatches)
		var fileOrder []string
		for _, m := range allMatches {
			fm, ok := fileMap[m.Path]
			if !ok {
				fm = &FileMatches{Path: m.Path}
				fileMap[m.Path] = fm
				fileOrder = append(fileOrder, m.Path)
			}
			fm.Matches = append(fm.Matches, m)
			fm.Count++
		}
		r.Files = make([]FileMatches, 0, len(fileOrder))
		for _, path := range fileOrder {
			r.Files = append(r.Files, *fileMap[path])
		}
		r.TotalCount = len(allMatches)
	} else {
		// For files_with_matches and count modes, offset/limit applies to files
		if opts.Offset > 0 {
			if opts.Offset >= len(r.Files) {
				r.Files = nil
			} else {
				r.Files = r.Files[opts.Offset:]
			}
		}
		if opts.HeadLimit > 0 && len(r.Files) > opts.HeadLimit {
			r.Files = r.Files[:opts.HeadLimit]
			r.Truncated = true
		}
		r.TotalCount = 0
		for _, fm := range r.Files {
			r.TotalCount += fm.Count
		}
	}
	return r
}

// shouldRespectGitignore determines whether to honor .gitignore rules.
func shouldRespectGitignore(opts GrepOptions) bool {
	if opts.RespectGitignore != nil {
		return *opts.RespectGitignore
	}
	// Auto: respect gitignore if the path is inside a git repo
	dir := opts.Path
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// matchGlob matches a relative path against a glob pattern using doublestar,
// which supports ** recursive matching and {alt1,alt2} brace expansion.
// For patterns without a path separator (e.g. *.go, *.{ts,tsx}), the pattern
// is anchored to match at any depth, mirroring ripgrep's --glob behavior.
func matchGlob(pattern, rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.ContainsRune(pattern, '/') {
		pattern = "**/" + pattern
	}
	matched, _ := doublestar.Match(pattern, rel)
	return matched
}
