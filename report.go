package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type coverage struct {
	ScannedRoots                    []string       `json:"scannedRoots"`
	CandidateCountByName            map[string]int `json:"candidateCountByName"`
	SkippedPathCounts               map[string]int `json:"skippedPathCounts"`
	SkippedPathSamples              []skippedPath  `json:"skippedPathSamples"`
	UnsupportedEvidenceSourceCounts map[string]int `json:"unsupportedEvidenceSourceCounts"`
}

func buildCoverage(roots []string, candidates []candidateFile, skipped []skippedPath) coverage {
	counts := map[string]int{}
	unsupported := map[string]int{}
	for _, candidate := range candidates {
		counts[candidate.Name]++
		if candidate.Name == "bun.lockb" {
			unsupported[candidate.Name]++
		}
	}
	skippedCounts := map[string]int{}
	for _, item := range skipped {
		skippedCounts[item.Reason]++
	}
	if len(skipped) > 10 {
		skipped = skipped[:10]
	}
	return coverage{
		ScannedRoots:                    roots,
		CandidateCountByName:            counts,
		SkippedPathCounts:               skippedCounts,
		SkippedPathSamples:              skipped,
		UnsupportedEvidenceSourceCounts: unsupported,
	}
}

func dedupeFindings(items []finding) []finding {
	seen := map[string]struct{}{}
	var out []finding
	for _, item := range items {
		key := item.Category + "|" + item.Indicator + "|" + item.Result + "|" + item.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func applyStrictMode(items []finding, cov coverage) []finding {
	out := make([]finding, 0, len(items))
	for _, item := range items {
		if item.Result == "NoIssue" {
			item.Result = "NeedsReview"
			if len(cov.UnsupportedEvidenceSourceCounts) > 0 || len(cov.SkippedPathCounts) > 0 {
				item.AssessmentDetail += "; strict mode promoted due to coverage gaps"
			} else {
				item.AssessmentDetail += "; strict mode promoted for manual review"
			}
		}
		out = append(out, item)
	}
	return out
}

func sortFindings(items []finding) {
	sort.Slice(items, func(i, j int) bool {
		if resultRank(items[i].Result) != resultRank(items[j].Result) {
			return resultRank(items[i].Result) < resultRank(items[j].Result)
		}
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Path < items[j].Path
	})
}

func resultRank(result string) int {
	switch result {
	case "Problem":
		return 0
	case "NeedsReview":
		return 1
	default:
		return 2
	}
}

type assessment struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

func overallAssessment(items []finding, cov coverage, strict bool) assessment {
	for _, item := range items {
		if item.Result == "Problem" {
			return assessment{Result: "Problem", Reason: "One or more high-confidence findings were detected."}
		}
	}
	for _, item := range items {
		if item.Result == "NeedsReview" {
			return assessment{Result: "NeedsReview", Reason: "Review findings or unsupported evidence sources exist."}
		}
	}
	if strict && (len(cov.SkippedPathCounts) > 0 || len(cov.UnsupportedEvidenceSourceCounts) > 0) {
		return assessment{Result: "NeedsReview", Reason: "Strict mode treats coverage gaps as review blockers."}
	}
	return assessment{Result: "NoIssue", Reason: "No target package evidence was detected in supported sources."}
}

type projectSummary struct {
	ProjectName  string    `json:"projectName,omitempty"`
	ProjectRoot  string    `json:"projectRoot"`
	Result       string    `json:"result"`
	FindingCount int       `json:"findingCount"`
	Findings     []finding `json:"findings"`
}

func buildProjectSummaries(contexts []projectContext, items []finding) []projectSummary {
	grouped := map[string][]finding{}
	projectNames := map[string]string{}
	for _, ctx := range contexts {
		projectNames[ctx.ProjectRoot] = ctx.ProjectName
	}
	for _, item := range items {
		root := item.ProjectRoot
		if root == "" {
			root = item.Root
		}
		grouped[root] = append(grouped[root], item)
	}

	var projects []projectSummary
	for root, findings := range grouped {
		sortFindings(findings)
		result := "NoIssue"
		if len(findings) > 0 {
			result = findings[0].Result
		}
		projects = append(projects, projectSummary{
			ProjectName:  projectNames[root],
			ProjectRoot:  root,
			Result:       result,
			FindingCount: len(findings),
			Findings:     findings,
		})
	}
	sort.Slice(projects, func(i, j int) bool {
		if resultRank(projects[i].Result) != resultRank(projects[j].Result) {
			return resultRank(projects[i].Result) < resultRank(projects[j].Result)
		}
		return projects[i].ProjectRoot < projects[j].ProjectRoot
	})
	return projects
}

func writeOutputs(base string, findings []finding, cov coverage, projects []projectSummary) error {
	if err := writeCSV(base+".csv", findings); err != nil {
		return err
	}
	if err := writeJSON(base+".findings.json", findings); err != nil {
		return err
	}
	if err := writeJSON(base+".coverage.json", cov); err != nil {
		return err
	}
	return writeJSON(base+".projects.json", projects)
}

func writeCSV(path string, findings []finding) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"Category", "Indicator", "AssessmentDetail", "LocationRisk", "Result", "Root", "Path", "ProjectName", "ProjectRoot"})
	for _, item := range findings {
		_ = w.Write([]string{item.Category, item.Indicator, item.AssessmentDetail, item.LocationRisk, item.Result, item.Root, item.Path, item.ProjectName, item.ProjectRoot})
	}
	return w.Error()
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func printOverall(item assessment) {
	fmt.Printf("\nOverall assessment: %s\n", item.Result)
	fmt.Printf("Reason            : %s\n\n", item.Reason)
}

func printSummary(items []finding) {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Result]++
	}
	fmt.Println("Summary:")
	for _, key := range []string{"Problem", "NeedsReview", "NoIssue"} {
		fmt.Printf("  %-12s %d\n", key, counts[key])
	}
	fmt.Println()
}

func printProjects(projects []projectSummary) {
	fmt.Println("Projects:")
	for _, project := range projects {
		label := project.ProjectName
		if label == "" {
			label = project.ProjectRoot
		}
		fmt.Printf("  %-12s %s (%d findings)\n", project.Result, label, project.FindingCount)
	}
	fmt.Println()
}

func printCoverage(cov coverage) {
	fmt.Println("Coverage:")
	keys := make([]string, 0, len(cov.CandidateCountByName))
	for key := range cov.CandidateCountByName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-20s %d\n", key, cov.CandidateCountByName[key])
	}
	if len(cov.UnsupportedEvidenceSourceCounts) > 0 {
		fmt.Println("Unsupported evidence:")
		for key, count := range cov.UnsupportedEvidenceSourceCounts {
			fmt.Printf("  %-20s %d\n", key, count)
		}
	}
	fmt.Println()
}
