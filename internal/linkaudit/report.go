package linkaudit

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func WriteJSON(w io.Writer, results []Result) error {
	copyResults := append([]Result(nil), results...)
	sortResults(copyResults)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(copyResults)
}

func WriteMarkdown(w io.Writer, results []Result) error {
	copyResults := append([]Result(nil), results...)
	sortResults(copyResults)
	if _, err := fmt.Fprintln(w, "| Tool | Version | OS | Arch | Role | Status | URL | Final URL | Archive | SHA verified | Checked at |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|"); err != nil {
		return err
	}
	for _, r := range copyResults {
		values := []string{r.Tool, r.Version, r.OS, r.Arch, r.Role, r.FailureReason, r.SourceURL, r.FinalURL, r.ArchiveDetected, fmt.Sprint(r.SHA256Matches), r.CheckedAt.Format("2006-01-02T15:04:05Z")}
		for i := range values {
			values[i] = strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|").Replace(values[i])
		}
		if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(values, " | ")); err != nil {
			return err
		}
	}
	return nil
}
