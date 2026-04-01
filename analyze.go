package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type occurrence struct {
	Name    string
	Version string
	Detail  string
}

type finding struct {
	Category         string `json:"category"`
	Indicator        string `json:"indicator"`
	AssessmentDetail string `json:"assessmentDetail"`
	LocationRisk     string `json:"locationRisk"`
	Result           string `json:"result"`
	Root             string `json:"root"`
	Path             string `json:"path"`
	ProjectName      string `json:"projectName,omitempty"`
	ProjectRoot      string `json:"projectRoot,omitempty"`
}

func analyzeCandidates(candidates []candidateFile, targets []targetSpec, contexts []projectContext, throttle int) ([]finding, []skippedPath, error) {
	targetMap := groupTargets(targets)
	inCh := make(chan candidateFile)
	outCh := make(chan []finding, 64)
	skipCh := make(chan []skippedPath, 64)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for range throttle {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range inCh {
				records, skips, err := analyzeCandidate(candidate, targetMap, contexts)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					continue
				}
				outCh <- records
				skipCh <- skips
			}
		}()
	}

	go func() {
		for _, candidate := range candidates {
			inCh <- candidate
		}
		close(inCh)
		wg.Wait()
		close(outCh)
		close(skipCh)
	}()

	var findings []finding
	var skipped []skippedPath
	for outCh != nil || skipCh != nil {
		select {
		case chunk, ok := <-outCh:
			if !ok {
				outCh = nil
				continue
			}
			findings = append(findings, chunk...)
		case chunk, ok := <-skipCh:
			if !ok {
				skipCh = nil
				continue
			}
			skipped = append(skipped, chunk...)
		}
	}
	select {
	case err := <-errCh:
		return nil, nil, err
	default:
	}
	return findings, skipped, nil
}

func groupTargets(targets []targetSpec) map[string][]targetSpec {
	out := make(map[string][]targetSpec, len(targets))
	for _, target := range targets {
		out[strings.ToLower(target.Name)] = append(out[strings.ToLower(target.Name)], target)
	}
	return out
}

func analyzeCandidate(candidate candidateFile, targets map[string][]targetSpec, contexts []projectContext) ([]finding, []skippedPath, error) {
	data, err := os.ReadFile(candidate.Path)
	if err != nil {
		ctx := nearestProjectContext(candidate.Path, contexts)
		return []finding{makeFinding(candidate, ctx, "UnreadableEvidence", candidate.Name, "evidence source could not be read", "High", "NeedsReview")}, []skippedPath{{Path: candidate.Path, Stage: "analysis", Reason: "read failed: " + err.Error()}}, nil
	}
	content := string(data)
	switch strings.ToLower(candidate.Name) {
	case "package-lock.json", "npm-shrinkwrap.json":
		occurrences, parseErr := parsePackageLock(content)
		return findingsOrFailure(candidate, occurrences, parseErr, targets, contexts, candidate.Name)
	case "pnpm-lock.yaml":
		occurrences, parseErr := parsePnpmLock(content)
		return findingsOrFailure(candidate, occurrences, parseErr, targets, contexts, candidate.Name)
	case "yarn.lock":
		occurrences, parseErr := parseYarnLock(content)
		return findingsOrFailure(candidate, occurrences, parseErr, targets, contexts, candidate.Name)
	case "bun.lock":
		occurrences, parseErr := parseBunLock(content)
		return findingsOrFailure(candidate, occurrences, parseErr, targets, contexts, candidate.Name)
	case "bun.lockb":
		ctx := nearestProjectContext(candidate.Path, contexts)
		return []finding{makeFinding(candidate, ctx, "UnsupportedEvidence", "bun.lockb", "binary Bun lockfile detected but not parsed", "Medium", "NeedsReview")}, nil, nil
	case "package.json":
		findings, parseErr := parsePackageManifest(candidate, content, targets, contexts)
		if parseErr != nil {
			ctx := nearestProjectContext(candidate.Path, contexts)
			return []finding{makeFinding(candidate, ctx, "UnreadableEvidence", "package.json", "package manifest could not be parsed", "Medium", "NeedsReview")}, []skippedPath{{Path: candidate.Path, Stage: "analysis", Reason: "parse failed: " + parseErr.Error()}}, nil
		}
		return findings, nil, nil
	default:
		return nil, nil, nil
	}
}

func findingsOrFailure(candidate candidateFile, occurrences []occurrence, parseErr error, targets map[string][]targetSpec, contexts []projectContext, indicator string) ([]finding, []skippedPath, error) {
	if parseErr != nil {
		ctx := nearestProjectContext(candidate.Path, contexts)
		return []finding{makeFinding(candidate, ctx, "UnreadableEvidence", indicator, "evidence source could not be parsed", "High", "NeedsReview")}, []skippedPath{{Path: candidate.Path, Stage: "analysis", Reason: "parse failed: " + parseErr.Error()}}, nil
	}
	return findingsFromOccurrences(candidate, occurrences, targets, contexts), nil, nil
}

func findingsFromOccurrences(candidate candidateFile, occurrences []occurrence, targets map[string][]targetSpec, contexts []projectContext) []finding {
	ctx := nearestProjectContext(candidate.Path, contexts)
	var findings []finding
	var matched []occurrence
	for _, occ := range occurrences {
		if matchesTarget(occ, targets) {
			matched = append(matched, occ)
			findings = append(findings, makeFinding(candidate, ctx, "Lockfile", indicator(occ), occ.Detail, "Medium", "NoIssue"))
		}
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, occ := range matched {
			names = append(names, indicator(occ))
		}
		sort.Strings(names)
		findings = append(findings, makeFinding(candidate, ctx, "Correlation", strings.Join(names, " + "), "same evidence source contains multiple target packages", "High", "Problem"))
	}
	return findings
}

func parsePackageManifest(candidate candidateFile, content string, targets map[string][]targetSpec, contexts []projectContext) ([]finding, error) {
	var manifest struct {
		Name                 string            `json:"name"`
		Version              string            `json:"version"`
		Scripts              map[string]string `json:"scripts"`
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, err
	}
	ctx := nearestProjectContext(candidate.Path, contexts)
	var findings []finding
	if manifest.Name != "" && matchesTarget(occurrence{Name: manifest.Name, Version: manifest.Version}, targets) {
		detail := "package manifest matched target package"
		if postinstall, ok := manifest.Scripts["postinstall"]; ok && postinstall != "" {
			detail += "; summary: " + summarize(postinstall, 40)
		}
		findings = append(findings, makeFinding(candidate, ctx, "InstalledPackage", indicator(occurrence{Name: manifest.Name, Version: manifest.Version}), detail, "Medium", "NoIssue"))
	}

	var matched []occurrence
	for _, deps := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.OptionalDependencies} {
		for name, version := range deps {
			occ := occurrence{Name: name, Version: normalizeManifestVersion(version), Detail: "package.json dependency reference"}
			if matchesTarget(occ, targets) {
				matched = append(matched, occ)
				findings = append(findings, makeFinding(candidate, ctx, "PackageManifestDependency", indicator(occ), occ.Detail, "Low", "NoIssue"))
			}
		}
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, occ := range matched {
			names = append(names, indicator(occ))
		}
		sort.Strings(names)
		findings = append(findings, makeFinding(candidate, ctx, "Correlation", strings.Join(names, " + "), "package manifest references multiple target packages", "Medium", "Problem"))
	}
	return findings, nil
}

func normalizeManifestVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimLeft(raw, "^~=<>= ")
	return strings.Trim(raw, "\"'")
}

func parsePackageLock(content string) ([]occurrence, error) {
	var doc struct {
		Packages map[string]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	var out []occurrence
	for path, pkg := range doc.Packages {
		if path == "" {
			continue
		}
		name := pkg.Name
		if name == "" {
			name = filepath.Base(path)
		}
		if name != "" && pkg.Version != "" {
			out = append(out, occurrence{Name: name, Version: pkg.Version, Detail: "resolved package entry found in package-lock"})
		}
	}
	for name, dep := range doc.Dependencies {
		if dep.Version != "" {
			out = append(out, occurrence{Name: name, Version: dep.Version, Detail: "dependency entry found in package-lock"})
		}
	}
	return out, nil
}

func parseBunLock(content string) ([]occurrence, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	var out []occurrence
	var walk func(any, string)
	walk = func(value any, key string) {
		switch node := value.(type) {
		case map[string]any:
			name, _ := node["name"].(string)
			version, _ := node["version"].(string)
			if name == "" && key != "" {
				name = splitPackageRef(key).Name
			}
			if name != "" && version != "" {
				out = append(out, occurrence{Name: name, Version: version, Detail: "resolved package entry found in bun.lock"})
			}
			for childKey, childValue := range node {
				walk(childValue, childKey)
			}
		case []any:
			for _, item := range node {
				walk(item, key)
			}
		}
	}
	walk(doc, "")
	return out, nil
}

func parsePnpmLock(content string) ([]occurrence, error) {
	var doc struct {
		Packages  map[string]any `yaml:"packages"`
		Importers map[string]struct {
			Dependencies map[string]struct {
				Version string `yaml:"version"`
			} `yaml:"dependencies"`
		} `yaml:"importers"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	var out []occurrence
	for ref := range doc.Packages {
		spec := splitPackageRef(strings.Trim(strings.TrimPrefix(ref, "/"), "'"))
		if spec.Name != "" && spec.Version != "" {
			out = append(out, occurrence{Name: spec.Name, Version: spec.Version, Detail: "resolved package entry found in pnpm-lock"})
		}
	}
	for _, importer := range doc.Importers {
		for name, dep := range importer.Dependencies {
			version := strings.Split(strings.TrimSpace(dep.Version), "(")[0]
			version = strings.TrimPrefix(version, "npm:")
			if version != "" {
				out = append(out, occurrence{Name: name, Version: version, Detail: "dependency entry found in pnpm importer"})
			}
		}
	}
	return out, nil
}

func parseYarnLock(content string) ([]occurrence, error) {
	var out []occurrence
	for _, block := range strings.Split(content, "\n\n") {
		lines := strings.Split(block, "\n")
		if len(lines) == 0 {
			continue
		}
		header := strings.TrimSpace(strings.TrimSuffix(lines[0], ":"))
		if header == "" {
			continue
		}
		version := ""
		for _, line := range lines[1:] {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "version ") {
				version = strings.Trim(strings.TrimPrefix(line, "version "), "\"")
				break
			}
		}
		if version == "" {
			continue
		}
		for _, selector := range strings.Split(header, ",") {
			spec := splitPackageRef(strings.Trim(selector, "\" "))
			if spec.Name != "" {
				out = append(out, occurrence{Name: spec.Name, Version: version, Detail: "resolved package entry found in yarn.lock"})
			}
		}
	}
	return out, nil
}

type pkgRef struct {
	Name    string
	Version string
}

func splitPackageRef(raw string) pkgRef {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pkgRef{}
	}
	if strings.HasPrefix(raw, "@") {
		idx := strings.LastIndex(raw, "@")
		if idx > 0 {
			return pkgRef{Name: raw[:idx], Version: strings.Split(raw[idx+1:], "(")[0]}
		}
		return pkgRef{Name: raw}
	}
	if idx := strings.LastIndex(raw, "@"); idx > 0 {
		return pkgRef{Name: raw[:idx], Version: strings.Split(raw[idx+1:], "(")[0]}
	}
	return pkgRef{Name: raw}
}

func matchesTarget(occ occurrence, targets map[string][]targetSpec) bool {
	specs := targets[strings.ToLower(occ.Name)]
	if len(specs) == 0 {
		return false
	}
	for _, spec := range specs {
		if spec.Version == "" || strings.EqualFold(spec.Version, occ.Version) {
			return true
		}
	}
	return false
}

func indicator(occ occurrence) string {
	if occ.Version == "" {
		return occ.Name
	}
	return occ.Name + "@" + occ.Version
}

func makeFinding(candidate candidateFile, ctx *projectContext, category, indicator, detail, risk, result string) finding {
	item := finding{
		Category:         category,
		Indicator:        indicator,
		AssessmentDetail: detail,
		LocationRisk:     risk,
		Result:           result,
		Root:             candidate.Root,
		Path:             candidate.Path,
	}
	if ctx != nil {
		item.ProjectName = ctx.ProjectName
		item.ProjectRoot = ctx.ProjectRoot
		if ctx.ProjectName != "" {
			item.AssessmentDetail += "; project: " + ctx.ProjectName
		}
	}
	return item
}

func analyzeNodeModules(roots []string, targets []targetSpec, contexts []projectContext, excludedPaths []string) ([]finding, []skippedPath, error) {
	targetMap := groupTargets(targets)
	var findings []finding
	var skipped []skippedPath
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				skipped = append(skipped, skippedPath{Path: path, Stage: "node_modules", Reason: err.Error()})
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if isExcludedPath(path, excludedPaths) || shouldSkipDir(path) {
				return filepath.SkipDir
			}
			if filepath.Base(filepath.Dir(path)) != "node_modules" {
				return nil
			}
			name := filepath.Base(path)
			if _, ok := targetMap[strings.ToLower(name)]; !ok {
				return nil
			}
			pkgPath := filepath.Join(path, "package.json")
			data, readErr := os.ReadFile(pkgPath)
			candidate := candidateFile{Root: root, Path: pkgPath, Name: "package.json"}
			ctx := nearestProjectContext(path, contexts)
			if readErr != nil {
				findings = append(findings, makeFinding(candidate, ctx, "NodeModulesFolder", name, "directory exists under node_modules but package.json could not be read", "High", "NeedsReview"))
				return filepath.SkipDir
			}
			var manifest struct {
				Name    string            `json:"name"`
				Version string            `json:"version"`
				Scripts map[string]string `json:"scripts"`
			}
			if err := json.Unmarshal(data, &manifest); err != nil {
				findings = append(findings, makeFinding(candidate, ctx, "NodeModulesFolder", name, "directory exists under node_modules but package.json could not be parsed", "High", "NeedsReview"))
				return filepath.SkipDir
			}
			detail := "package.json under node_modules matched installed package name/version"
			if postinstall, ok := manifest.Scripts["postinstall"]; ok && postinstall != "" {
				detail += "; summary: " + summarize(postinstall, 40)
			}
			findings = append(findings, makeFinding(candidate, ctx, "NodeModulesPackage", indicator(occurrence{Name: manifest.Name, Version: manifest.Version}), detail, "High", "Problem"))
			return filepath.SkipDir
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return findings, skipped, nil
}

func analyzeNpmLogs(targets []targetSpec) []finding {
	logDir := npmLogDir()
	if logDir == "" {
		return nil
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil
	}
	targetMap := groupTargets(targets)
	var findings []finding
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".log") {
			continue
		}
		path := filepath.Join(logDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			lower := strings.ToLower(line)
			for name, specs := range targetMap {
				if !strings.Contains(lower, name) {
					continue
				}
				for _, spec := range specs {
					if spec.Version != "" && !strings.Contains(line, spec.Version) {
						continue
					}
					ind := spec.Name
					if spec.Version != "" {
						ind += "@" + spec.Version
					}
					findings = append(findings, finding{
						Category:         "NpmLog",
						Indicator:        ind,
						AssessmentDetail: "npm cache log contains target package reference",
						LocationRisk:     "Low",
						Result:           "NeedsReview",
						Root:             logDir,
						Path:             path,
					})
				}
			}
		}
	}
	return findings
}

func npmLogDir() string {
	if cache := os.Getenv("npm_config_cache"); cache != "" {
		return filepath.Join(cache, "_logs")
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "npm", "_logs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LocalAppData"); local != "" {
			return filepath.Join(local, "npm-cache", "_logs")
		}
	}
	return filepath.Join(home, ".npm", "_logs")
}

func summarize(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
