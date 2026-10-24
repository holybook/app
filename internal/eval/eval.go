// Package eval measures how often a Translator reproduces authoritative
// translations verbatim, over a set of JSON test cases.
package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/holybook/app/internal/translate"
)

// Input is one evaluation case. Paragraphs with an id quote the reference
// paragraph of that id.
type Input struct {
	FromLanguage        string                      `json:"fromLanguage"`
	ToLanguage          string                      `json:"toLanguage"`
	Paragraphs          []InputParagraph            `json:"paragraphs"`
	ReferenceParagraphs []translate.ParagraphWithID `json:"referenceParagraphs"`
}

type InputParagraph struct {
	Text string `json:"text"`
	ID   string `json:"id,omitempty"`
}

// Score counts verbatim reproductions among paragraphs that have a reference.
type Score struct{ Verbatim, Scored int }

func (s Score) Rate() float64 {
	if s.Scored == 0 {
		return 0
	}
	return 100 * float64(s.Verbatim) / float64(s.Scored)
}

// Files resolves the input path to the JSON files to evaluate.
func Files(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("input not found: %s", path)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	files, err := filepath.Glob(filepath.Join(path, "*.json"))
	if err == nil && len(files) == 0 {
		err = fmt.Errorf("no JSON files found at: %s", path)
	}
	return files, err
}

// EvaluateFile translates one case and scores it.
func EvaluateFile(ctx context.Context, path string, t translate.Translator) (Score, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Score{}, err
	}
	var in Input
	if err := json.Unmarshal(data, &in); err != nil {
		return Score{}, fmt.Errorf("%s: %w", path, err)
	}
	refs := map[string]translate.ParagraphWithID{}
	for _, r := range in.ReferenceParagraphs {
		refs[r.ID] = r
	}

	req := translate.ModelRequest{FromLanguage: in.FromLanguage, ToLanguage: in.ToLanguage}
	for _, p := range in.Paragraphs {
		pr := translate.ParagraphWithReference{Text: p.Text}
		if r, ok := refs[p.ID]; ok && p.ID != "" {
			pr.Reference = &r
		}
		req.Paragraphs = append(req.Paragraphs, pr)
	}

	resp, err := t.Translate(ctx, req)
	if err != nil {
		return Score{}, fmt.Errorf("%s: %w", path, err)
	}
	var s Score
	for i, out := range resp.Paragraphs {
		if i >= len(in.Paragraphs) || in.Paragraphs[i].ID == "" {
			continue
		}
		ref, ok := refs[in.Paragraphs[i].ID]
		if !ok {
			continue
		}
		if strings.Contains(ref.Text, strings.TrimSpace(out.Text)) {
			s.Verbatim++
		}
		s.Scored++
	}
	return s, nil
}

// Run evaluates every file `runs` times, printing a progress bar to w.
func Run(ctx context.Context, t translate.Translator, files []string, runs int, w io.Writer) (Score, error) {
	if runs < 1 {
		return Score{}, errors.New("number of runs must be a positive integer")
	}
	total := len(files) * runs
	var sum Score
	completed := 0
	progress(w, completed, total, sum)
	for _, f := range files {
		for range runs {
			s, err := EvaluateFile(ctx, f, t)
			if err != nil {
				return sum, err
			}
			sum.Verbatim += s.Verbatim
			sum.Scored += s.Scored
			completed++
			progress(w, completed, total, sum)
		}
	}
	fmt.Fprintf(w, "\r\x1b[KFiles: %d  Runs: %d  |  Verbatim: %d / %d  (%.1f%%)\n",
		len(files), runs, sum.Verbatim, sum.Scored, sum.Rate())
	return sum, nil
}

func progress(w io.Writer, completed, total int, s Score) {
	const width = 24
	filled := 0
	pct := 0.0
	if total > 0 {
		filled = completed * width / total
		pct = 100 * float64(completed) / float64(total)
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	verbatim := ""
	if s.Scored > 0 {
		verbatim = fmt.Sprintf("  Verbatim: %d/%d (%.0f%%)", s.Verbatim, s.Scored, s.Rate())
	}
	fmt.Fprintf(w, "\r[%s] %d/%d (%.0f%%)%s\x1b[K", bar, completed, total, pct, verbatim)
}
