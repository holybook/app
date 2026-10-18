package book

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Descriptor locates the original source document of one book. Descriptors are
// stored one per line in the index files of the holybook/data repository:
//
//	<language> <author> <id> <url> [<publishedAt>]
type Descriptor struct {
	ID       string
	Language string
	Author   string
	URL      string
	// ExplicitPublishedAt is set when the source provides a date; see PublishedAt.
	ExplicitPublishedAt time.Time
}

// PublishedAt is the explicit publication date or, for Universal House of
// Justice messages, the date encoded in their id (e.g. "uhj20200722_001").
// Other books without an explicit date have none (zero value).
func (d Descriptor) PublishedAt() time.Time {
	if !d.ExplicitPublishedAt.IsZero() {
		return d.ExplicitPublishedAt
	}
	if d.Author == "uhj" && len(d.ID) >= 11 {
		if t, err := time.Parse("20060102", d.ID[3:11]); err == nil {
			return t
		}
	}
	return time.Time{}
}

// FilePath is where the book's XML lives below a content directory:
// <language>/<author>[/<year>]/<id>.xml.
func (d Descriptor) FilePath(contentDir string) string {
	parts := []string{contentDir, d.Language, d.Author}
	if t := d.PublishedAt(); !t.IsZero() {
		parts = append(parts, strconv.Itoa(t.Year()))
	}
	return filepath.Join(append(parts, d.ID+".xml")...)
}

// String renders the descriptor as an index file line (without newline).
func (d Descriptor) String() string {
	s := d.Language + " " + d.Author + " " + d.ID + " " + d.URL
	if !d.ExplicitPublishedAt.IsZero() {
		s += " " + d.ExplicitPublishedAt.Format(DateLayout)
	}
	return s
}

// ParseDescriptor parses one index file line.
func ParseDescriptor(line string) (Descriptor, error) {
	parts := strings.Split(line, " ")
	if len(parts) < 4 {
		return Descriptor{}, fmt.Errorf("invalid descriptor %q", line)
	}
	d := Descriptor{Language: parts[0], Author: parts[1], ID: parts[2], URL: parts[3]}
	if len(parts) > 4 {
		t, err := time.Parse(DateLayout, parts[4])
		if err != nil {
			return Descriptor{}, fmt.Errorf("invalid descriptor date in %q: %w", line, err)
		}
		d.ExplicitPublishedAt = t
	}
	return d, nil
}

// ReadDescriptors parses an index file, skipping blank lines.
func ReadDescriptors(r io.Reader) ([]Descriptor, error) {
	var result []Descriptor
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		d, err := ParseDescriptor(line)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, s.Err()
}

// WriteDescriptors writes an index file.
func WriteDescriptors(w io.Writer, descriptors []Descriptor) error {
	bw := bufio.NewWriter(w)
	for _, d := range descriptors {
		bw.WriteString(d.String() + "\n")
	}
	return bw.Flush()
}
