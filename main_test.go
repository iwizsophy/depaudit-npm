package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTargetLine(t *testing.T) {
	tests := []struct {
		line    string
		name    string
		version string
	}{
		{"axios", "axios", ""},
		{"axios@1.14.1", "axios", "1.14.1"},
		{"@scope/pkg", "@scope/pkg", ""},
		{"@scope/pkg@2.0.0", "@scope/pkg", "2.0.0"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.line, func(t *testing.T) {
			got, err := parseTargetLine(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.name || got.Version != tt.version {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestParseOptionsSupportsRepeatedAndSpaceSeparatedValues(t *testing.T) {
	opts, err := parseOptions([]string{
		"--roots", "repoA", "repoB,repoC",
		"--exclude-paths-file", "one.txt", "two.txt;three.txt",
		"--targets-file", "targets.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(opts.roots, "|"); got != "repoA|repoB|repoC" {
		t.Fatalf("unexpected roots: %v", opts.roots)
	}
	if got := strings.Join(opts.excludePathFiles, "|"); got != "one.txt|two.txt|three.txt" {
		t.Fatalf("unexpected exclude files: %v", opts.excludePathFiles)
	}
}

func TestFixturePackageLockCorrelation(t *testing.T) {
	base := filepath.Join("testdata", "fixtures", "package-lock-correlation")
	findings, coverage, projects, stdout := runFixture(t, base, []string{"plain-crypto-js@4.2.1", "axios@1.14.1"}, true, false, false)
	assertContainsResult(t, findings, "Correlation", "Problem")
	assertProjectResult(t, projects, "sample-app", "Problem")
	assertStdoutContains(t, stdout, "Overall assessment: Problem")
	if coverage.CandidateCountByName["package-lock.json"] != 1 {
		t.Fatalf("unexpected coverage: %+v", coverage.CandidateCountByName)
	}
	for _, project := range projects {
		if project.ProjectName == "plain-crypto-js" {
			t.Fatalf("node_modules package should not become a project summary: %+v", projects)
		}
	}
}

func TestFixturePnpmFalsePositive(t *testing.T) {
	base := filepath.Join("testdata", "fixtures", "pnpm-false-positive")
	findings, _, _, stdout := runFixture(t, base, []string{"plain-crypto-js@4.2.1", "axios@1.14.1"}, false, false, false)
	if len(findings) != 0 {
		t.Fatalf("expected zero findings, got %d", len(findings))
	}
	assertStdoutContains(t, stdout, "Overall assessment: NoIssue")
}

func TestFixtureStrictPromotion(t *testing.T) {
	base := filepath.Join("testdata", "fixtures", "strict-promotion")
	findings, _, _, stdout := runFixture(t, base, []string{"axios@1.14.1"}, false, false, true)
	found := false
	for _, item := range findings {
		if item.Indicator == "axios@1.14.1" && item.Result == "NeedsReview" {
			found = true
		}
	}
	if !found {
		t.Fatalf("strict promotion not applied: %+v", findings)
	}
	assertStdoutContains(t, stdout, "Overall assessment: NeedsReview")
}

func TestFixtureUnsupportedBun(t *testing.T) {
	base := filepath.Join("testdata", "fixtures", "unsupported-bun")
	findings, coverage, _, _ := runFixture(t, base, []string{"plain-crypto-js@4.2.1"}, false, false, false)
	assertContainsIndicator(t, findings, "UnsupportedEvidence", "bun.lockb")
	if coverage.UnsupportedEvidenceSourceCounts["bun.lockb"] != 1 {
		t.Fatalf("unexpected unsupported counts: %+v", coverage.UnsupportedEvidenceSourceCounts)
	}
}

func TestFixtureBunLockCorrelation(t *testing.T) {
	base := filepath.Join("testdata", "fixtures", "bun-lock-correlation")
	findings, coverage, _, _ := runFixture(t, base, []string{"plain-crypto-js@4.2.1", "axios@1.14.1"}, false, false, false)
	assertContainsResult(t, findings, "Correlation", "Problem")
	if coverage.CandidateCountByName["bun.lock"] != 1 {
		t.Fatalf("unexpected coverage bun.lock count: %+v", coverage.CandidateCountByName)
	}
}

func TestDefaultTargetsFileIsLoadedWhenOmitted(t *testing.T) {
	tempDir := t.TempDir()
	if err := copyFixture(filepath.Join("testdata", "fixtures", "strict-promotion"), tempDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, defaultTargetsFile), []byte("axios@1.14.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(tempDir, "out")
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--roots", ".", "--output-dir", outputDir}); err != nil {
		t.Fatal(err)
	}
	findingsPath := findOutputFile(t, outputDir, ".findings.json")
	var findings []finding
	readJSON(t, findingsPath, &findings)
	assertContainsIndicator(t, findings, "Lockfile", "axios@1.14.1")
}

func TestMalformedEvidenceSourceProducesNeedsReviewAndCoverageGap(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, defaultTargetsFile), []byte("axios@1.14.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "package-lock.json"), []byte("{ invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(tempDir, "out")
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--roots", ".", "--output-dir", outputDir}); err != nil {
		t.Fatal(err)
	}
	findingsPath := findOutputFile(t, outputDir, ".findings.json")
	coveragePath := findOutputFile(t, outputDir, ".coverage.json")
	var findings []finding
	readJSON(t, findingsPath, &findings)
	assertContainsIndicator(t, findings, "UnreadableEvidence", "package-lock.json")
	var cov coverage
	readJSON(t, coveragePath, &cov)
	if len(cov.SkippedPathCounts) == 0 {
		t.Fatalf("expected skipped path counts to capture parse failure: %+v", cov)
	}
}

func runFixture(t *testing.T, fixtureDir string, targets []string, includeNodeModules bool, includeLogs bool, strict bool) ([]finding, coverage, []projectSummary, string) {
	t.Helper()
	tempDir := t.TempDir()
	targetPath := filepath.Join(tempDir, "targets.txt")
	if err := os.WriteFile(targetPath, []byte(strings.Join(targets, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(tempDir, "out")
	args := []string{
		"--roots", fixtureDir,
		"--targets-file", targetPath,
		"--output-dir", outputDir,
		"--throttle-limit", "1",
	}
	if includeNodeModules {
		args = append(args, "--include-node-modules-folder-check")
	}
	if includeLogs {
		args = append(args, "--include-npm-cache-logs")
	}
	if strict {
		args = append(args, "--strict")
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := run(args)
	_ = w.Close()
	os.Stdout = oldStdout

	data, err := io.ReadAll(r)
	if err != nil {
		_ = r.Close()
		t.Fatal(err)
	}
	_ = r.Close()
	if runErr != nil {
		t.Fatal(runErr)
	}

	findingsPath := findOutputFile(t, outputDir, ".findings.json")
	coveragePath := findOutputFile(t, outputDir, ".coverage.json")
	projectsPath := findOutputFile(t, outputDir, ".projects.json")

	var findings []finding
	readJSON(t, findingsPath, &findings)
	var cov coverage
	readJSON(t, coveragePath, &cov)
	var projects []projectSummary
	readJSON(t, projectsPath, &projects)
	return findings, cov, projects, string(data)
}

func findOutputFile(t *testing.T, dir string, suffix string, excluded ...string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		skip := false
		for _, ex := range excluded {
			if strings.HasSuffix(name, ex) {
				skip = true
				break
			}
		}
		if !skip {
			return filepath.Join(dir, name)
		}
	}
	t.Fatalf("no output file with suffix %s", suffix)
	return ""
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func assertContainsResult(t *testing.T, findings []finding, category, result string) {
	t.Helper()
	for _, item := range findings {
		if item.Category == category && item.Result == result {
			return
		}
	}
	t.Fatalf("missing finding category=%s result=%s", category, result)
}

func assertContainsIndicator(t *testing.T, findings []finding, category, indicator string) {
	t.Helper()
	for _, item := range findings {
		if item.Category == category && item.Indicator == indicator {
			return
		}
	}
	t.Fatalf("missing finding category=%s indicator=%s", category, indicator)
}

func assertProjectResult(t *testing.T, projects []projectSummary, name, result string) {
	t.Helper()
	for _, project := range projects {
		if project.ProjectName == name && project.Result == result {
			return
		}
	}
	t.Fatalf("missing project summary %s=%s", name, result)
}

func assertStdoutContains(t *testing.T, stdout, needle string) {
	t.Helper()
	if !strings.Contains(stdout, needle) {
		t.Fatalf("stdout did not contain %q\n%s", needle, stdout)
	}
}

func copyFixture(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
