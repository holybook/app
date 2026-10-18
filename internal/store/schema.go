package store

// SchemaVersion identifies the database schema. Bump it whenever the schema
// changes: databases imported with another version are reimported by the next
// content sync (see AppliedCommit), and the server warns about them.
const SchemaVersion = "2"

// Author is a known author with display names per language.
type Author struct {
	ID    string            `json:"id"`
	Names map[string]string `json:"names"`
}

// SeedAuthors is the registry of known authors in their canonical display
// order. The importer writes it to the database; books by authors missing here
// are still imported, with the author code as display name.
var SeedAuthors = []Author{
	{"bab", map[string]string{"en": "The Báb", "de": "Der Báb"}},
	{"bahaullah", map[string]string{"en": "Bahá’u’lláh", "de": "Bahá’u’lláh"}},
	{"abdulbaha", map[string]string{"en": "‘Abdu’l-Bahá", "de": "‘Abdu’l-Bahá"}},
	{"shoghieffendi", map[string]string{"en": "Shoghi Effendi", "de": "Shoghi Effendi"}},
	{"uhj", map[string]string{"en": "The Universal House of Justice", "de": "Das Universale Haus der Gerechtigkeit"}},
	{"compilation", map[string]string{"en": "Compilations", "de": "Zusammenstellungen"}},
}

// dropSchema removes all content tables. sync_state is deliberately kept: it
// records which content commit is imported and must survive reimports.
const dropSchema = `
DROP TABLE IF EXISTS paragraphs;
DROP TABLE IF EXISTS translations;
DROP TABLE IF EXISTS books;
DROP TABLE IF EXISTS author_names;
DROP TABLE IF EXISTS authors;
DROP FUNCTION IF EXISTS search_config(text);
`

// createSchema creates the content tables. The full-text index is created by
// the importer after loading.
//
// search_config maps a language code to its full-text search configuration. It
// is the single place this mapping lives: the generated text_tokens column and
// the search queries both use it, so the stored vectors and the queries can
// never disagree.
const createSchema = `
CREATE FUNCTION search_config(language text) RETURNS regconfig
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT CASE language
    WHEN 'en' THEN 'english'::regconfig
    WHEN 'de' THEN 'german'::regconfig
    ELSE 'simple'::regconfig
  END
$$;

CREATE TABLE authors (
  id       VARCHAR(32) PRIMARY KEY,
  position INT NOT NULL
);

CREATE TABLE author_names (
  author   VARCHAR(32) NOT NULL REFERENCES authors(id),
  language VARCHAR(3) NOT NULL,
  name     VARCHAR(256) NOT NULL,
  PRIMARY KEY (author, language)
);

CREATE TABLE books (
  id           VARCHAR(128) PRIMARY KEY,
  author       VARCHAR(32) NOT NULL REFERENCES authors(id),
  published_at DATE NULL
);

CREATE TABLE translations (
  book     VARCHAR(128) NOT NULL REFERENCES books(id),
  language VARCHAR(3) NOT NULL,
  title    VARCHAR(512) NOT NULL,
  PRIMARY KEY (book, language)
);

CREATE TABLE paragraphs (
  book         VARCHAR(128) NOT NULL,
  language     VARCHAR(3) NOT NULL,
  index        INT NOT NULL,
  number       INT NULL,
  section_path VARCHAR(64) NOT NULL DEFAULT '',
  type         VARCHAR(64) NOT NULL,
  text         TEXT NOT NULL,
  text_tokens  tsvector GENERATED ALWAYS AS (to_tsvector(search_config(language), text)) STORED,
  PRIMARY KEY (book, language, index),
  FOREIGN KEY (book, language) REFERENCES translations(book, language)
);
`

const createSyncState = `CREATE TABLE IF NOT EXISTS sync_state (key TEXT PRIMARY KEY, value TEXT NOT NULL)`
