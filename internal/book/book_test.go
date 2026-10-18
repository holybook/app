package book

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func parse(t *testing.T, xml string) *Content {
	t.Helper()
	c, err := ReadXML(strings.NewReader(xml))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func filter(ps []Paragraph, typ ParagraphType) []Paragraph {
	var out []Paragraph
	for _, p := range ps {
		if p.Type == typ {
			out = append(out, p)
		}
	}
	return out
}

func labels(ps []Paragraph) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Label())
	}
	return out
}

func TestFlatBookKeepsBareNumbering(t *testing.T) {
	c := parse(t, `<book id="x" language="en" title="T" author="uhj" publishedAt="2020-03-20">
  <p type="letter-head">Head</p>
  <p>First.</p>
  <p>Second.</p>
</book>`)

	if c.ID != "x" || c.Language != "en" || c.Title != "T" || c.Author != "uhj" {
		t.Errorf("unexpected metadata %+v", c)
	}
	if !c.PublishedAt.Equal(time.Date(2020, 3, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("publishedAt = %v", c.PublishedAt)
	}
	body := filter(c.Paragraphs, Body)
	if got := labels(body); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Errorf("labels = %v", got)
	}
	if c.Paragraphs[0].Type != LetterHead || c.Paragraphs[0].Number != nil {
		t.Errorf("letter head = %+v", c.Paragraphs[0])
	}
	if n := len(filter(c.Paragraphs, SectionTitle)); n != 0 {
		t.Errorf("section titles = %d", n)
	}
}

func TestNestedSectionsRestartNumbering(t *testing.T) {
	c := parse(t, `<book id="x" language="en" title="T" author="abu">
  <section title="One">
    <p>a</p>
    <p>b</p>
    <section title="One-one">
      <p>c</p>
    </section>
    <p>d</p>
  </section>
  <section title="Two">
    <p>e</p>
  </section>
</book>`)

	var titles []string
	for _, p := range filter(c.Paragraphs, SectionTitle) {
		titles = append(titles, p.Text+"="+p.SectionPath)
	}
	if want := []string{"One=1", "One-one=1.1", "Two=2"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}

	body := filter(c.Paragraphs, Body)
	if got, want := labels(body), []string{"1:1", "1:2", "1.1:1", "1:3", "2:1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
	for i, p := range c.Paragraphs {
		if p.Index != i {
			t.Errorf("paragraph %d has index %d", i, p.Index)
		}
	}
}

func TestUnknownTypeFallsBackToBody(t *testing.T) {
	c := parse(t, `<book id="x"><p type="whatever">a</p></book>`)
	if c.Paragraphs[0].Type != Body {
		t.Errorf("type = %v", c.Paragraphs[0].Type)
	}
}

func TestHeadingNesting(t *testing.T) {
	b := NewBuilder()
	b.Add("intro", Body)
	b.Heading(2, "A")
	b.Add("a1", Body)
	b.Heading(3, "A.1")
	b.Add("a11", Body)
	b.Heading(3, "A.2")
	b.Heading(2, "B")
	b.Add("b1", Body)

	var got []string
	for _, p := range b.Paragraphs() {
		got = append(got, p.Text+"@"+p.SectionPath)
	}
	want := []string{"intro@", "A@1", "a1@1", "A.1@1.1", "a11@1.1", "A.2@1.2", "B@2", "b1@2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestXMLRoundTrip(t *testing.T) {
	once := parse(t, `<book id="x" language="en" title="T &amp; &quot;U&quot;" author="abu">
  <section title="One">
    <p type="body">a &lt; b</p>
    <section title="One-one">
      <p type="body">b</p>
    </section>
    <p type="body">c</p>
  </section>
</book>`)

	var buf bytes.Buffer
	if err := once.WriteXML(&buf); err != nil {
		t.Fatal(err)
	}
	twice, err := ReadXML(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(once, twice) {
		t.Errorf("round trip changed the book:\n%+v\n%+v", once, twice)
	}
}

// TestCorpusRoundTrip checks that writing a parsed book reproduces
// the files of the holybook/data repository exactly, so fetch-content output
// stays diff-friendly. Set HOLYBOOK_CONTENT to the content directory to run it.
func TestCorpusRoundTrip(t *testing.T) {
	dir := os.Getenv("HOLYBOOK_CONTENT")
	if dir == "" {
		t.Skip("HOLYBOOK_CONTENT not set")
	}
	n, formatOnly := 0, 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".xml" {
			return err
		}
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		c, err := ReadXML(bytes.NewReader(original))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		var buf bytes.Buffer
		if err := c.WriteXML(&buf); err != nil {
			return err
		}
		got := buf.Bytes()
		reread, err := ReadXML(bytes.NewReader(got))
		if err != nil || !reflect.DeepEqual(c, reread) {
			t.Errorf("%s: round trip changed the content (%v)", path, err)
		} else if !bytes.Equal(got, original) {
			// Hand-edited files may use other indentation; only the content must match.
			formatOnly++
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("checked %d files, %d differ only in formatting", n, formatOnly)
	if formatOnly > n/10 {
		t.Errorf("too many files differ in formatting: %d of %d", formatOnly, n)
	}
}

func TestDescriptor(t *testing.T) {
	d, err := ParseDescriptor("en uhj uhj20200722_001 https://example.org/x.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.PublishedAt().Format(DateLayout); got != "2020-07-22" {
		t.Errorf("inferred date = %s", got)
	}
	if got := d.FilePath("content"); got != filepath.Join("content", "en", "uhj", "2020", "uhj20200722_001.xml") {
		t.Errorf("path = %s", got)
	}
	if d.String() != "en uhj uhj20200722_001 https://example.org/x.xhtml" {
		t.Errorf("string = %s", d.String())
	}

	d, err = ParseDescriptor("en bahaullah bahaullah_x https://example.org/x.xhtml 1873-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if d.PublishedAt().Year() != 1873 || !strings.HasSuffix(d.String(), " 1873-01-01") {
		t.Errorf("explicit date not kept: %+v", d)
	}

	d, _ = ParseDescriptor("en bahaullah bahaullah_y https://example.org/y.xhtml")
	if !d.PublishedAt().IsZero() || d.FilePath("c") != filepath.Join("c", "en", "bahaullah", "bahaullah_y.xml") {
		t.Errorf("undated descriptor: %+v %s", d, d.FilePath("c"))
	}
}
