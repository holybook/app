package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/holybook/go/internal/book"
	"github.com/holybook/go/internal/eval"
	"github.com/holybook/go/internal/fetch"
	"github.com/holybook/go/internal/translate"
)

// xmlFiles yields the .xml files below dir in lexical order.
func xmlFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".xml") {
			files = append(files, path)
		}
		return nil
	})
	if err == nil && len(files) == 0 {
		err = fmt.Errorf("no .xml files found in %s", dir)
	}
	return files, err
}

func readBooks(files []string) iter.Seq2[*book.Content, error] {
	return func(yield func(*book.Content, error) bool) {
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				yield(nil, err)
				return
			}
			c, err := book.ReadXML(f)
			f.Close()
			if err != nil {
				err = fmt.Errorf("%s: %w", path, err)
			}
			if !yield(c, err) {
				return
			}
		}
	}
}

func runImport(ctx context.Context, args []string) error {
	fs := newFlagSet("import")
	input := fs.String("i", "../data/content", "content directory (book XML files, searched recursively)")
	commit := fs.String("commit", "", "content commit to record as applied after a successful import")
	ifChanged := fs.Bool("if-changed", false, "skip the import if -commit is already imported with the current schema")
	db := addDBFlags(fs, "server")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := db.open(ctx)
	if err != nil {
		return err
	}
	defer st.Close()

	if *ifChanged && *commit != "" {
		applied, err := st.AppliedCommit(ctx)
		if err != nil {
			return err
		}
		if applied == *commit {
			slog.Info("database already up to date", "commit", applied)
			return nil
		}
		slog.Info("database out of date", "applied", applied, "commit", *commit)
	}

	files, err := xmlFiles(*input)
	if err != nil {
		return err
	}

	start := time.Now()
	slog.Info("importing", "files", len(files), "dir", *input)
	stats, err := st.Import(ctx, readBooks(files), *commit)
	if err != nil {
		return err
	}
	slog.Info("import complete", "books", stats.Books, "translations", stats.Translations,
		"paragraphs", stats.Paragraphs, "commit", *commit, "duration", time.Since(start).Round(time.Millisecond))
	return nil
}

func fetchIndex(ctx context.Context, args []string) error {
	fs := newFlagSet("fetch-index")
	output := fs.String("o", "../data/index", "target directory for the index files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return fetch.FetchIndex(ctx, *output)
}

func fetchContent(ctx context.Context, args []string) error {
	fs := newFlagSet("fetch-content")
	input := fs.String("i", "../data/index", "index file, or directory of index files")
	output := fs.String("o", "../data/content", "target content directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var files []string
	info, err := os.Stat(*input)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(*input)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, filepath.Join(*input, e.Name()))
			}
		}
	} else {
		files = []string{*input}
	}
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		descriptors, err := book.ReadDescriptors(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		slog.Info("processing descriptors", "count", len(descriptors), "file", filepath.Base(path))
		fetch.FetchAll(ctx, descriptors, *output)
	}
	return ctx.Err()
}

// bookIndex is the JSON written by the index command.
type bookIndex struct {
	Metadata map[string]bookMetadata `json:"metadata"`
}

type bookMetadata struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

func index(ctx context.Context, args []string) error {
	fs := newFlagSet("index")
	input := fs.String("i", "../data/content", "content directory")
	output := fs.String("o", "index.json", "output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, err := xmlFiles(*input)
	if err != nil {
		return err
	}
	idx := bookIndex{Metadata: map[string]bookMetadata{}}
	i := 0
	for c, err := range readBooks(files) {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(*input, files[i])
		i++
		key := c.ID
		if _, dup := idx.Metadata[key]; dup {
			key = c.ID + "." + c.Language
		}
		idx.Metadata[key] = bookMetadata{Title: c.Title, Path: filepath.ToSlash(rel)}
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	slog.Info("writing index", "books", len(idx.Metadata), "file", *output)
	return os.WriteFile(*output, append(data, '\n'), 0o644)
}

func evaluate(ctx context.Context, args []string) error {
	fs := newFlagSet("eval")
	apiKey := fs.String("k", "", "Gemini API key (default: $GEMINI_API_KEY)")
	model := fs.String("m", env("GEMINI_MODEL", translate.DefaultModel), "model name")
	input := fs.String("i", "", "input JSON file or directory of JSON files (required)")
	translatorName := fs.String("t", "", "translator: monolithic or incremental")
	paragraphTranslator := fs.String("p", "", "paragraph translator for an incremental translator: gemini")
	textMatcher := fs.String("x", "", "text matcher for an incremental translator: gemini")
	runs := fs.Int("n", 1, "number of times to repeat the evaluation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return errors.New("input file is required (-i)")
	}
	key := *apiKey
	if key == "" {
		key = os.Getenv("GEMINI_API_KEY")
	}
	g, err := translate.NewGemini(ctx, key, *model)
	if err != nil {
		return err
	}

	var t translate.Translator
	switch {
	case *translatorName == "monolithic":
		t = translate.Monolithic{Gemini: g}
	case *translatorName == "incremental" || (*translatorName == "" && *paragraphTranslator == "gemini" && *textMatcher == "gemini"):
		t = translate.Incremental{
			Translator: translate.GeminiParagraphTranslator{Gemini: g},
			Matcher:    translate.GeminiTextMatcher{Gemini: g},
		}
	case *translatorName != "":
		return fmt.Errorf("translator name not found: %s", *translatorName)
	default:
		return errors.New("provide a translator (-t) or a paragraph translator (-p) and text matcher (-x); the only implementation of both is gemini")
	}

	files, err := eval.Files(*input)
	if err != nil {
		return err
	}
	slog.Info("evaluating", "translator", fmt.Sprintf("%T", t), "files", len(files), "runs", *runs)
	_, err = eval.Run(ctx, t, files, *runs, os.Stdout)
	return err
}
