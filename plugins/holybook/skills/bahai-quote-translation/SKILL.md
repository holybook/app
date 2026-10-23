---
name: bahai-quote-translation
description: Translate texts that quote the Bahá'í writings (talks, articles, letters, newsletters, study and devotional materials) so that every quotation uses the official authoritative translation instead of a new one, with sources. Use whenever translating a text that contains or may contain quotations from Bahá'u'lláh, the Báb, 'Abdu'l-Bahá, Shoghi Effendi or the Universal House of Justice, or when asked for the official translation of a passage of the writings.
---

# Translating texts that quote the Bahá'í writings

**Principle:** when an authoritative (officially published) translation of a passage exists, it is
used verbatim. It is never retranslated, polished, shortened differently or paraphrased. Only the
text around the quotations is translated freely. A passage with no authoritative translation in the
target language is translated by you and marked as a *provisional translation*.

## Tool (holybook MCP server)

`find_authoritative_translation(paragraphs, from_language, to_language)` checks, for each given
paragraph or quotation, whether it quotes the writings in the source language and returns:

- `status`: `authoritative` (verbatim quotation found), `possibly_authoritative` (similar or
  partially overlapping text — verify) or `not_found`.
- `matches[]`, best first, each with
  - `match`: `exact` (input is a verbatim, possibly abridged excerpt), `close` (same passage, small
    wording differences) or `partial` (only part of the input overlaps);
  - `inputCoverage` / `paragraphCoverage`: how much of the input is in the source paragraph and how
    much of the source paragraph is in the input;
  - `source`: the authoritative paragraph in the source language with citation and link;
  - `translation`: if `available`, the translated book's `title` and the authoritative translation
    of the whole source `paragraph`, with citation and link. Otherwise a `reason`:
    `not_translated` (the work has no translation in the target language in this library) or
    `paragraph_count_mismatch` (the language versions of the work are not structured alike, so the
    corresponding paragraph cannot be identified reliably).

Supported languages are listed in the tool schema.

## Workflow

1. **Identify the languages** (ISO codes, e.g. `en`, `de`). If the target language is not
   supported, authoritative translations cannot be looked up: translate everything yourself, mark
   all quotations as provisional translations and tell the user.
2. **Split the text and pick what to look up**, keeping the original order:
   - Quotations (block quotes, text in quotation marks): pass only the quoted words — without
     quotation marks, attribution ("Bahá'u'lláh writes:") or reference.
   - Paragraphs that may be unmarked quotations (scriptural style, compilation entries, prayers):
     pass the whole paragraph.
   - A quotation spanning several paragraphs: pass each paragraph separately.
   - Plain prose without quotations does not need a lookup; when unsure, include it — a
     `not_found` costs little.
3. **Look up in batches** of up to 20 entries, ideally one call for the whole text.
4. **Choose the authoritative wording** for every `authoritative` or `possibly_authoritative`
   result:
   1. Use the first match whose `translation.available` is true; `translation.paragraph.text` is
      the authoritative translation of `source.text`.
   2. If the input covers the whole source paragraph (`paragraphCoverage` ≈ 1), use the whole
      translated paragraph.
   3. If the input is only part of the source paragraph, find the contiguous span of the
      translation that corresponds to exactly that part and copy it character for character
      (spelling, punctuation, capitalization, transliteration). Mark omissions with "…" where the
      input has them. Only the outer quotation marks may follow the target language's conventions.
   4. For `close` matches, the input is a slightly different rendering of the same passage (e.g.
      an older translation or a typo): still use the authoritative translation, and mention the
      discrepancy if it matters.
   5. For `partial` matches, the input contains more than the quotation, or the quotation spans
      several paragraphs: extract the quoted part and handle it as above (look it up again on its
      own if unsure); translate the rest normally.
   6. The same passage is often quoted in several works (e.g. a verse of Bahá'u'lláh in a letter of
      the Universal House of Justice), so there may be several matches. Prefer the translation of
      the original work; otherwise prefer a rendering that quotes the passage as a whole rather
      than one adapted to the grammar of its surrounding sentence (with inserted `[ist]` or `..`).
      Cite the work you took the translation from ("quoted in …" if it is not the original).
   7. If no match has `translation.available` (for either `reason`), the passage is authoritative
      but its translation could not be retrieved from this library. Do not try to locate the
      translated paragraph yourself, e.g. with `get_passage`: for `paragraph_count_mismatch` the
      versions do not correspond paragraph by paragraph. The library also does not contain every
      published translation, so say "no authoritative translation found in the library" rather
      than "none exists". Translate the passage yourself, faithfully and in the elevated register
      of the existing translations, and mark it as provisional translation.
5. **Handle `not_found`:** translate normally. If the text is presented as a quotation of the
   writings but was not found, translate it provisionally and point this out — it may come from a
   source outside this library, a pilgrim's note, or be misattributed.
6. **Translate the rest of the text** naturally and consistently with the authoritative passages:
   use the terminology of the official translations (e.g. names of institutions, titles of works)
   and the standard transliteration of names (Bahá'u'lláh, ‘Abdu'l-Bahá, Riḍván).
7. **Deliver** the complete translation, preserving the structure of the original, followed by a
   short list of the quotations used with their status and sources:

   ```
   Quellen
   1. „<Anfang des Zitats> …“ — <Autor>, <Titel> (<Datum>), Abs. <Nummer> — autorisierte
      Übersetzung — <Link>
   2. „<Anfang des Zitats> …“ — <Autor>, <Titel>, Abs. <Nummer> — vorläufige Übersetzung (keine
      autorisierte Übersetzung in der Bibliothek gefunden)
   ```

   Write this list in the target language.

   Mark provisional translations in the text too (e.g. a footnote), as is customary in Bahá'í
   publications.

## Single passages

The same tool answers "What is the official German translation of this quotation?": look it up,
return the authoritative wording with its source, or say that none was found.
