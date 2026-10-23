package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/holybook/go/internal/quote"
	"github.com/holybook/go/internal/store"
)

const maxPassageLength = 40

func (h *Handler) addResearchTools(s *mcp.Server, cat catalog) {
	addReadOnlyTool(s, &mcp.Tool{
		Name:  "search_writings",
		Title: "Search the Bahá’í writings",
		Description: `Full-text search of the authoritative Bahá’í writings: the works of Bahá’u’lláh, the Báb and ‘Abdu’l-Bahá, the writings of Shoghi Effendi, the messages of the Universal House of Justice and official compilations, as published in the Bahá’í Reference Library. Returns ranked paragraphs with their exact text, citation (book, author, paragraph number) and a link.

Each call searches one language; write the query in that language. Words are stemmed and combined with AND. Use "double quotes" for exact phrases, ` + "`or`" + ` for alternatives and a leading ` + "`-`" + ` to exclude a word, e.g. ` + "`consultation \"spiritual assembly\" -youth`" + `. Prefer two or three distinctive words over long queries; if there are no results, try synonyms or fewer words. Write names and terms with their diacritics as in the texts (Bahá’u’lláh, ‘Abdu’l-Bahá, Riḍván, Bahá’í, Kitáb-i-Aqdas): unaccented spellings such as "Bahaullah" do not match. Use get_passage to read the context of a result before relying on it.`,
		InputSchema: objectSchema(map[string]schema{
			"query":    stringSchema("Search terms in the selected language.", nil, ""),
			"language": stringSchema("Language of the query and of the searched texts.", cat.languages, "en"),
			"author":   stringSchema("Only search texts by this author: "+cat.authorDescription()+".", cat.authorIDs(), ""),
			"book_id":  stringSchema("Only search within this book.", nil, ""),
			"limit":    integerSchema("Maximum number of results.", 1, ptr(50), ptr(10)),
			"offset":   integerSchema("Number of results to skip, for paging.", 0, nil, ptr(0)),
		}, "query"),
	}, func(ctx context.Context, args *arguments) (any, error) {
		q := store.ParagraphQuery{
			Query:    args.string("query"),
			Language: args.choice("language", cat.languages, "en"),
			Author:   args.optionalChoice("author", cat.authorIDs()),
			BookID:   args.optionalString("book_id"),
			Limit:    args.int("limit", 10, 1, 50),
			Offset:   args.int("offset", 0, 0, 10_000),
		}
		if err := args.Err(); err != nil {
			return nil, err
		}
		page, err := h.store.FindParagraphs(ctx, q)
		if err != nil {
			return nil, err
		}
		res := searchResponse{
			Query:      q.Query,
			Language:   q.Language,
			Total:      page.Total,
			Offset:     q.Offset,
			Results:    make([]searchHit, len(page.Paragraphs)),
			NextOffset: nextOffset(q.Offset, q.Limit, page.Total),
		}
		for i, p := range page.Paragraphs {
			res.Results[i] = h.links.searchHit(p)
		}
		if len(page.Paragraphs) == 0 && q.Offset == 0 {
			res.Hint = "No results. Try fewer or different words, synonyms, names with diacritics " +
				"(e.g. Bahá’u’lláh, Riḍván), another language (available: " +
				strings.Join(cat.languages, ", ") + ") or remove filters."
		}
		return res, nil
	})

	addReadOnlyTool(s, &mcp.Tool{
		Name:        "get_passage",
		Title:       "Read a passage",
		Description: fmt.Sprintf(`Reads consecutive paragraphs of a book, e.g. the context around a search result (start a few paragraphs before its index) or a complete short letter. Paragraph indices are positions within the book in one language: they can differ between translations of the same book. Also returns the book's metadata and the languages it is available in. At most %d paragraphs per call.`, maxPassageLength),
		InputSchema: objectSchema(map[string]schema{
			"book_id":     stringSchema("The book id, as returned by search_writings or list_books.", nil, ""),
			"language":    stringSchema("Language of the book.", cat.languages, "en"),
			"start_index": integerSchema("Index of the first paragraph.", 0, nil, ptr(0)),
			"end_index":   integerSchema("Index of the last paragraph (inclusive). Defaults to start_index + 9.", 0, nil, nil),
		}, "book_id"),
	}, func(ctx context.Context, args *arguments) (any, error) {
		bookID := args.string("book_id")
		language := args.choice("language", cat.languages, "en")
		start := args.int("start_index", 0, 0, 1_000_000)
		end, ok := args.optionalInt("end_index", start, 1_000_000)
		if !ok {
			end = start + 9
		}
		if err := args.Err(); err != nil {
			return nil, err
		}
		if end-start+1 > maxPassageLength {
			return nil, argErrorf("At most %d paragraphs can be read per call.", maxPassageLength)
		}
		b, err := h.store.Book(ctx, bookID)
		if errors.Is(err, store.ErrNotFound) {
			return nil, argErrorf("Unknown book '%s'.", bookID)
		}
		if err != nil {
			return nil, err
		}
		t, ok := b.Translation(language)
		if !ok {
			return nil, argErrorf("Book '%s' is not available in '%s', only in: %s.",
				bookID, language, strings.Join(b.Languages(), ", "))
		}
		counts, err := h.store.ParagraphCounts(ctx, []string{bookID})
		if err != nil {
			return nil, err
		}
		paragraphs, err := h.store.Paragraphs(ctx, bookID, language, &start, &end)
		if err != nil {
			return nil, err
		}
		res := passageResponse{
			Book:               h.links.bookResult(t),
			ParagraphCount:     counts[bookID][language],
			AvailableLanguages: b.Languages(),
			Paragraphs:         make([]paragraphResult, len(paragraphs)),
		}
		for i, p := range paragraphs {
			res.Paragraphs[i] = h.links.paragraphResult(t, p)
		}
		if end+1 < res.ParagraphCount {
			res.NextIndex = ptr(end + 1)
		}
		return res, nil
	})

	addReadOnlyTool(s, &mcp.Tool{
		Name:        "list_books",
		Title:       "List books",
		Description: `Lists the books, tablets, letters and compilations available in a language, optionally filtered by author or by words in the title. Useful to find a specific work (e.g. the Kitáb-i-Íqán or a Riḍván message of a given year) and to restrict search_writings to it. Results are ordered by author, then newest first and by title.`,
		InputSchema: objectSchema(map[string]schema{
			"language":       stringSchema("Language of the titles.", cat.languages, "en"),
			"author":         stringSchema("Only books by this author: "+cat.authorDescription()+".", cat.authorIDs(), ""),
			"title_contains": stringSchema("Text the title must contain, ignoring case and diacritics.", nil, ""),
			"limit":          integerSchema("Maximum number of books.", 1, ptr(200), ptr(50)),
			"offset":         integerSchema("Number of books to skip, for paging.", 0, nil, ptr(0)),
		}),
	}, func(ctx context.Context, args *arguments) (any, error) {
		language := args.choice("language", cat.languages, "en")
		author := args.optionalChoice("author", cat.authorIDs())
		title := quote.Normalize(args.optionalString("title_contains"))
		limit := args.int("limit", 50, 1, 200)
		offset := args.int("offset", 0, 0, 10_000)
		if err := args.Err(); err != nil {
			return nil, err
		}
		library, err := h.store.Library(ctx, language)
		if err != nil {
			return nil, err
		}
		books := []bookResult{}
		for _, t := range library {
			if (author == "" || t.AuthorCode == author) && strings.Contains(quote.Normalize(t.Title), title) {
				books = append(books, h.links.bookResult(t))
			}
		}
		res := bookListResponse{
			Language:   language,
			Total:      len(books),
			Offset:     offset,
			Books:      books[min(offset, len(books)):min(offset+limit, len(books))],
			NextOffset: nextOffset(offset, limit, len(books)),
		}
		return res, nil
	})
}
