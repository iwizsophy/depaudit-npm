package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultOutputDir = "scan-result-fast"
const defaultTargetsFile = "targets.txt"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.showHelp {
		printHelp()
		return nil
	}

	targets, err := loadTargets(opts.targetsFile)
	if err != nil {
		return err
	}
	roots, err := normalizeRoots(opts.roots)
	if err != nil {
		return err
	}
	excludedPaths, err := loadExcludedPaths(opts.excludePathFiles)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(opts.outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	fmt.Printf("Roots         : %s\n", strings.Join(roots, ", "))
	fmt.Printf("TargetsFile   : %s\n", opts.targetsFile)
	fmt.Printf("ThrottleLimit : %d\n", opts.throttleLimit)
	fmt.Printf("OutputDir     : %s\n\n", mustAbs(opts.outputDir))

	fmt.Println("[1/4] Enumerating candidate files...")
	candidates, skipped, err := enumerateCandidates(roots, excludedPaths, opts.throttleLimit)
	if err != nil {
		return err
	}
	projectContexts := buildProjectContexts(candidates)
	fmt.Printf("Candidate files: %d\n\n", len(candidates))

	fmt.Println("[2/4] Analyzing candidate files...")
	findings, analysisSkipped, err := analyzeCandidates(candidates, targets, projectContexts, opts.throttleLimit)
	if err != nil {
		return err
	}
	skipped = append(skipped, analysisSkipped...)

	if opts.includeNodeModulesFolderCheck {
		fmt.Println("[3/4] Checking node_modules directories...")
		nodeFindings, nodeSkipped, err := analyzeNodeModules(roots, targets, projectContexts, excludedPaths)
		if err != nil {
			return err
		}
		findings = append(findings, nodeFindings...)
		skipped = append(skipped, nodeSkipped...)
	}

	if opts.includeNpmCacheLogs {
		fmt.Println("[4/4] Checking npm cache logs...")
		findings = append(findings, analyzeNpmLogs(targets)...)
	}

	coverage := buildCoverage(roots, candidates, skipped)
	findings = dedupeFindings(findings)
	if opts.strict {
		findings = applyStrictMode(findings, coverage)
	}
	sortFindings(findings)

	assessment := overallAssessment(findings, coverage, opts.strict)
	projects := buildProjectSummaries(projectContexts, findings)

	timestamp := nowStamp()
	base := filepath.Join(opts.outputDir, "npm_dependency_scan_"+timestamp)
	if err := writeOutputs(base, findings, coverage, projects); err != nil {
		return err
	}

	printOverall(assessment)
	printSummary(findings)
	printProjects(projects)
	printCoverage(coverage)
	fmt.Println("Output files:")
	fmt.Printf("  CSV : %s.csv\n", base)
	fmt.Printf("  JSON: %s.findings.json\n", base)
	fmt.Printf("  COV : %s.coverage.json\n", base)
	fmt.Printf("  PRJ : %s.projects.json\n", base)
	return nil
}

type options struct {
	roots                         []string
	outputDir                     string
	targetsFile                   string
	throttleLimit                 int
	includeNodeModulesFolderCheck bool
	includeNpmCacheLogs           bool
	strict                        bool
	excludePathFiles              []string
	showHelp                      bool
}

func parseOptions(args []string) (options, error) {
	opts := options{outputDir: defaultOutputDir, throttleLimit: defaultThrottle()}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			opts.showHelp = true
		case "--roots", "-r":
			values, next, err := collectMultiValue(args, i+1)
			if err != nil {
				return options{}, err
			}
			opts.roots = append(opts.roots, values...)
			i = next - 1
		case "--exclude-paths-file":
			values, next, err := collectMultiValue(args, i+1)
			if err != nil {
				return options{}, err
			}
			opts.excludePathFiles = append(opts.excludePathFiles, values...)
			i = next - 1
		case "--output-dir", "-o":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return options{}, errors.New("--output-dir requires a value")
			}
			i++
			opts.outputDir = args[i]
		case "--targets-file":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return options{}, errors.New("--targets-file requires a value")
			}
			i++
			opts.targetsFile = args[i]
		case "--throttle-limit", "-t":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return options{}, errors.New("--throttle-limit requires a value")
			}
			i++
			value, err := strconv.Atoi(args[i])
			if err != nil || value < 1 {
				return options{}, fmt.Errorf("invalid --throttle-limit: %s", args[i])
			}
			opts.throttleLimit = value
		case "--include-node-modules-folder-check":
			opts.includeNodeModulesFolderCheck = true
		case "--include-npm-cache-logs":
			opts.includeNpmCacheLogs = true
		case "--strict":
			opts.strict = true
		default:
			if strings.HasPrefix(arg, "-") {
				return options{}, fmt.Errorf("unknown option: %s", arg)
			}
			return options{}, fmt.Errorf("unexpected positional argument: %s", arg)
		}
	}
	if len(opts.roots) == 0 {
		opts.roots = []string{"."}
	}
	if opts.targetsFile == "" {
		resolved, err := resolveDefaultTargetsFile()
		if err != nil {
			return options{}, err
		}
		opts.targetsFile = resolved
	}
	return opts, nil
}

func collectMultiValue(args []string, start int) ([]string, int, error) {
	if start >= len(args) || strings.HasPrefix(args[start], "-") {
		return nil, start, errors.New("option requires at least one value")
	}
	values := make([]string, 0, 1)
	i := start
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		values = append(values, splitArgList(args[i])...)
		i++
	}
	if len(values) == 0 {
		return nil, start, errors.New("option requires at least one value")
	}
	return values, i, nil
}

func resolveDefaultTargetsFile() (string, error) {
	execPath, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(execPath), defaultTargetsFile)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	if _, err := os.Stat(defaultTargetsFile); err == nil {
		return defaultTargetsFile, nil
	}
	return "", fmt.Errorf("default targets file %q was not found beside the executable or in the current directory", defaultTargetsFile)
}

func printHelp() {
	fmt.Println("Usage:")
	fmt.Printf("  depaudit-npm [options]\n")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -r, --roots <path...>                   Scan roots. Repeatable; spaces, commas, and semicolons are supported.")
	fmt.Printf("      --targets-file <path>               Target package list file. Default: ./%s\n", defaultTargetsFile)
	fmt.Printf("  -o, --output-dir <path>                 Output directory. Default: ./%s\n", defaultOutputDir)
	fmt.Printf("  -t, --throttle-limit <n>                Parallelism. Default: %d\n", defaultThrottle())
	fmt.Println("      --include-node-modules-folder-check Include node_modules package checks")
	fmt.Println("      --include-npm-cache-logs            Include npm cache log scan")
	fmt.Println("      --exclude-paths-file <path...>      Load excluded subtree paths from text file. Repeatable.")
	fmt.Println("      --strict                            Promote clean-but-incomplete results to NeedsReview")
	fmt.Println("  -h, --help                              Show help")
	fmt.Println()
	fmt.Println("Targets file format:")
	fmt.Println("  One package spec per line: package, package@version, @scope/pkg, @scope/pkg@version")
}

type targetSpec struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

func loadTargets(path string) ([]targetSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read targets file: %w", err)
	}
	var targets []targetSpec
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		spec, err := parseTargetLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		targets = append(targets, spec)
	}
	if len(targets) == 0 {
		return nil, errors.New("targets file contains no package specs")
	}
	return targets, nil
}

func parseTargetLine(line string) (targetSpec, error) {
	if strings.HasPrefix(line, "@") {
		idx := strings.LastIndex(line, "@")
		if idx > 0 {
			return targetSpec{Name: line[:idx], Version: line[idx+1:]}, nil
		}
		return targetSpec{Name: line}, nil
	}
	if idx := strings.LastIndex(line, "@"); idx > 0 {
		return targetSpec{Name: line[:idx], Version: line[idx+1:]}, nil
	}
	return targetSpec{Name: line}, nil
}

func splitArgList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' })
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func defaultThrottle() int {
	if runtime.NumCPU() <= 2 {
		return 1
	}
	return runtime.NumCPU() - 2
}

func normalizeRoots(roots []string) ([]string, error) {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("invalid root %s: %w", abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("root is not a directory: %s", abs)
		}
		out = append(out, abs)
	}
	return out, nil
}

func loadExcludedPaths(files []string) ([]string, error) {
	var excluded []string
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		base := filepath.Dir(path)
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if !filepath.IsAbs(line) {
				line = filepath.Join(base, line)
			}
			abs, err := filepath.Abs(line)
			if err != nil {
				return nil, err
			}
			excluded = append(excluded, filepath.Clean(abs))
		}
	}
	return excluded, nil
}

func nowStamp() string {
	return time.Now().Format("20060102_150405")
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
