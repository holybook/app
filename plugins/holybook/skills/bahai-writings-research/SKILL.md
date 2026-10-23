---
name: bahai-writings-research
description: Research questions about the Bahá'í Faith in the authoritative Bahá'í writings (Bahá'u'lláh, the Báb, 'Abdu'l-Bahá, Shoghi Effendi, the Universal House of Justice) with the holybook MCP tools, and answer with exact, cited quotations. Use whenever someone asks what the Bahá'í writings say about a topic, asks for quotations or sources on a subject, wants a quotation verified or located, or asks about Bahá'í teachings, laws, institutions or history.
---

# Researching the Bahá'í writings

Answer questions about the Bahá'í Faith from the authoritative texts themselves: find the relevant
passages, read them in context, and answer with verbatim quotations, each cited and linked. Never
quote from memory — quotations that "sound right" are often misremembered or misattributed.

## Tools (holybook MCP server)

- `search_writings(query, language, author?, book_id?, limit?, offset?)` — keyword search, returns
  ranked paragraphs with text, citation and link.
- `get_passage(book_id, language, start_index, end_index?)` — reads consecutive paragraphs, e.g.
  the context of a search result.
- `list_books(language, author?, title_contains?)` — finds a specific work, e.g. to restrict a
  search to the Kitáb-i-Aqdas or to a Riḍván message of a certain year.

The supported languages and author ids are listed in the tool schemas.

## What the texts are

The library contains the authoritative texts as published in the Bahá'í Reference Library:

| Author id | Who | Standing |
| --- | --- | --- |
| `bahaullah`, `bab` | Bahá'u'lláh, the Báb | Revelation — the Word of God |
| `abdulbaha` | ‘Abdu'l-Bahá | Authorized interpreter; his writings are part of the sacred texts |
| `shoghieffendi` | Shoghi Effendi | Guardian, authorized interpreter |
| `uhj` | The Universal House of Justice | Supreme institution; legislates and elucidates |
| `compilation` | Compilations | Extracts from the above, arranged by topic |

When a passage appears both in a compilation and in the original work, cite the original. Pilgrims'
notes, secondary literature and individual opinions are not authoritative; do not present them as
the writings. Talks of ‘Abdu'l-Bahá (e.g. *Paris Talks*) are valued but are transcripts — say so if
it matters for the question.

## Workflow

1. **Plan the searches.** Identify the key concepts and list how the writings would phrase them.
   The search is keyword-based (with stemming), not semantic, so vocabulary matters: the writings
   use terms like *consultation*, *backbiting*, *chastity*, *the Covenant*, *Manifestation of God*,
   *Most Great Peace*, *the Lesser Peace*, *Spiritual Assembly*, *Huqúqu'lláh*. Write names and
   terms with their diacritics (Bahá'u'lláh, ‘Abdu'l-Bahá, Riḍván, Bahá'í): unaccented spellings
   like "Bahaullah" or "Ridvan" find nothing.
2. **Search broadly, then narrowly.** Run several short searches (2–3 distinctive words, or an
   exact `"phrase"`), with synonyms. Use `or` for alternatives and `-word` to exclude. Filter by
   `author` to see what each Central Figure or institution wrote on the subject. If `total` is
   large, add words or a phrase; if there are no results, use fewer or different words. English is
   the most complete language; when the user writes in another supported language, search in that
   language too (with translated search terms).
3. **Read in context.** For every passage you intend to use, call `get_passage` from a couple of
   paragraphs before its `index` to a couple after. Make sure the passage means what it appears to
   mean, see who is speaking and to whom, and note the date of letters.
4. **Answer.**
   - Lead with a direct answer, then support it with the most relevant quotations (usually 2–6),
     preferring the writings of Bahá'u'lláh, the Báb and ‘Abdu'l-Bahá and the authoritative
     interpretations of Shoghi Effendi, and the guidance of the Universal House of Justice for
     questions of administration and application.
   - Copy quotations character for character from the tool output. Mark omissions with "…".
   - Cite every quotation (see below) and keep your own summary or interpretation clearly separate
     from the quoted text. Individual understanding of the writings is personal, not authoritative.
   - If the texts you found do not settle the question, say so. Not finding something means it
     was not found in this library, not that the writings are silent on it.
5. **Verify quotations someone gives you** by searching for a distinctive `"exact phrase"` from it,
   then report the source — or that it could not be found, which may mean it is misattributed or
   not from the writings.

## Citation format

Use the fields from the tool results (`author`, `title`, `number`, `date`, `url`):

> "The earth is but one country, and mankind its citizens."
> — Bahá'u'lláh, *Gleanings from the Writings of Bahá'u'lláh*, ¶ 117:1 ([holybook.app](https://holybook.app/...))

For letters of the Universal House of Justice or Shoghi Effendi, add the date, e.g.
"— The Universal House of Justice, Riḍván 2010 message, ¶ 4". Omit the paragraph number if the
result has none.

## Languages

Answer in the user's language. If you quote a passage in another language than its source, use the
authoritative translation — look it up with `find_authoritative_translation` (see the
`bahai-quote-translation` skill). If none exists, translate it yourself and label it
"provisional translation". Paragraph indices differ between the language versions of a book, so do
not reuse an index from one language in another.

## Style

Write respectfully and use the standard transliteration of names and terms (Bahá'u'lláh, the Báb,
‘Abdu'l-Bahá, Shoghi Effendi, Kitáb-i-Aqdas, Riḍván), as in the tool results.
