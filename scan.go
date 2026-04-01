package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var candidateFileNames = map[string]struct{}{
	"package-lock.json":   {},
	"npm-shrinkwrap.json": {},
	"pnpm-lock.yaml":      {},
	"yarn.lock":           {},
	"package.json":        {},
	"bun.lock":            {},
	"bun.lockb":           {},
}

var excludedDirNames = map[string]struct{}{
	".git":        {},
	"dist":        {},
	"build":       {},
	"out":         {},
	"coverage":    {},
	".next":       {},
	".nuxt":       {},
	".cache":      {},
	".pnpm-store": {},
}

type candidateFile struct {
	Root         string    `json:"root"`
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	LastWriteUTC time.Time `json:"lastWriteUtc"`
}

type skippedPath struct {
	Path   string `json:"path"`
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

func enumerateCandidates(roots, excludedPaths []string, throttle int) ([]candidateFile, []skippedPath, error) {
	rootCh := make(chan string)
	candidateCh := make(chan candidateFile, 256)
	skipCh := make(chan skippedPath, 256)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for range throttle {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for root := range rootCh {
				if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						skipCh <- skippedPath{Path: path, Stage: "enumeration", Reason: err.Error()}
						if d != nil && d.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
					if isExcludedPath(path, excludedPaths) {
						if d.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
					if d.IsDir() {
						if shouldSkipDir(path) {
							return filepath.SkipDir
						}
						return nil
					}
					if _, ok := candidateFileNames[strings.ToLower(d.Name())]; !ok {
						return nil
					}
					info, infoErr := d.Info()
					if infoErr != nil {
						skipCh <- skippedPath{Path: path, Stage: "enumeration", Reason: infoErr.Error()}
						return nil
					}
					candidateCh <- candidateFile{
						Root:         root,
						Path:         filepath.Clean(path),
						Name:         d.Name(),
						LastWriteUTC: info.ModTime().UTC(),
					}
					return nil
				}); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
			}
		}()
	}

	go func() {
		for _, root := range roots {
			rootCh <- root
		}
		close(rootCh)
		wg.Wait()
		close(candidateCh)
		close(skipCh)
	}()

	var candidates []candidateFile
	var skipped []skippedPath
	for candidateCh != nil || skipCh != nil {
		select {
		case item, ok := <-candidateCh:
			if !ok {
				candidateCh = nil
				continue
			}
			candidates = append(candidates, item)
		case item, ok := <-skipCh:
			if !ok {
				skipCh = nil
				continue
			}
			skipped = append(skipped, item)
		}
	}

	select {
	case err := <-errCh:
		return nil, nil, err
	default:
	}

	return candidates, skipped, nil
}

func shouldSkipDir(path string) bool {
	_, ok := excludedDirNames[strings.ToLower(filepath.Base(path))]
	return ok
}

func isExcludedPath(path string, excludedPaths []string) bool {
	clean := filepath.Clean(path)
	for _, excluded := range excludedPaths {
		if clean == excluded || strings.HasPrefix(clean, excluded+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

type projectContext struct {
	ProjectRoot    string `json:"projectRoot"`
	ManifestPath   string `json:"manifestPath"`
	ProjectName    string `json:"projectName,omitempty"`
	ProjectVersion string `json:"projectVersion,omitempty"`
}

func buildProjectContexts(candidates []candidateFile) []projectContext {
	var contexts []projectContext
	for _, candidate := range candidates {
		if !strings.EqualFold(candidate.Name, "package.json") {
			continue
		}
		if pathHasSegment(candidate.Path, "node_modules") {
			continue
		}
		data, err := os.ReadFile(candidate.Path)
		if err != nil {
			continue
		}
		var manifest struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		contexts = append(contexts, projectContext{
			ProjectRoot:    filepath.Dir(candidate.Path),
			ManifestPath:   candidate.Path,
			ProjectName:    manifest.Name,
			ProjectVersion: manifest.Version,
		})
	}
	sort.Slice(contexts, func(i, j int) bool { return len(contexts[i].ProjectRoot) > len(contexts[j].ProjectRoot) })
	return contexts
}

func nearestProjectContext(path string, contexts []projectContext) *projectContext {
	for _, ctx := range contexts {
		if path == ctx.ManifestPath || strings.HasPrefix(path, ctx.ProjectRoot+string(os.PathSeparator)) {
			copy := ctx
			return &copy
		}
	}
	return nil
}

func pathHasSegment(path string, segment string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if strings.EqualFold(part, segment) {
			return true
		}
	}
	return false
}
