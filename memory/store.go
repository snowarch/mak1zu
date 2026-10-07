// Package memory is the companion's durable memory: one SQLite file with a
// full-text index. It is deliberately boring. Recall is always scoped to the
// current speaker plus explicitly global entries, so one person's private
// facts can never surface in another person's conversation.
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Kind string

const (
	Episodic      Kind = "episodic"      // something that happened
	Semantic      Kind = "semantic"      // a stable fact about a person or the world
	Introspective Kind = "introspective" // her own reflections; never shown as someone else's
)

// Global is the user id of memories every speaker may see.
const Global = ""

// Embedder is optional: with one, recall reranks full-text candidates by
// cosine similarity.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Store struct {
	db  *sql.DB
	emb Embedder
}

const schema = `
CREATE TABLE IF NOT EXISTS memories(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  persona TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  content TEXT NOT NULL,
  user_id TEXT NOT NULL DEFAULT '',
  importance REAL NOT NULL DEFAULT 0.5,
  score REAL NOT NULL DEFAULT 1.0,
  tags TEXT NOT NULL DEFAULT '',
  embedding BLOB,
  created TEXT NOT NULL,
  accessed TEXT NOT NULL,
  access_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_mem_user ON memories(user_id);
CREATE INDEX IF NOT EXISTS idx_mem_persona ON memories(persona);
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(content, tags, content='memories', content_rowid='id', tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER IF NOT EXISTS mem_ai AFTER INSERT ON memories BEGIN
  INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS mem_ad AFTER DELETE ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES('delete', old.id, old.content, old.tags);
END;
CREATE TRIGGER IF NOT EXISTS mem_au AFTER UPDATE OF content, tags ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES('delete', old.id, old.content, old.tags);
  INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
END;
CREATE TABLE IF NOT EXISTS people(
  user_id TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  names TEXT NOT NULL DEFAULT '[]',
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS relationships(
  user_id TEXT NOT NULL,
  persona TEXT NOT NULL,
  dynamic TEXT NOT NULL DEFAULT '',
  familiarity REAL NOT NULL DEFAULT 0,
  interactions INTEGER NOT NULL DEFAULT 0,
  inside_jokes TEXT NOT NULL DEFAULT '[]',
  last_seen TEXT NOT NULL,
  PRIMARY KEY(user_id, persona)
);
CREATE TABLE IF NOT EXISTS facts(
  user_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  updated TEXT NOT NULL,
  PRIMARY KEY(user_id, key)
);
CREATE TABLE IF NOT EXISTS turns(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  persona TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  user_msg TEXT NOT NULL,
  reply TEXT NOT NULL,
  ts TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_turns_user ON turns(user_id);
CREATE INDEX IF NOT EXISTS idx_turns_chan ON turns(channel_id);
CREATE TABLE IF NOT EXISTS reminders(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  content TEXT NOT NULL,
  due TEXT NOT NULL,
  done INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_rem_due ON reminders(done, due);
`

// Open creates the database (and its directory) with owner-only permissions.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec(schema + personSchema); err != nil {
		return nil, fmt.Errorf("memory schema: %w", err)
	}
	if err := migratePeople(db); err != nil {
		return nil, fmt.Errorf("memory migrate: %w", err)
	}
	if path != ":memory:" {
		_ = os.Chmod(path, 0o600)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error           { return s.db.Close() }
func (s *Store) SetEmbedder(e Embedder) { s.emb = e }
func (s *Store) DB() *sql.DB            { return s.db }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

type Memory struct {
	ID         int64
	Persona    string
	Kind       Kind
	Content    string
	UserID     string
	Importance float64
	Score      float64
	Tags       string
	Created    string
	Accessed   string
	Count      int
}

// Remember stores a memory. Near-duplicates (same user, same normalized text)
// bump the existing row instead of piling up.
func (s *Store) Remember(ctx context.Context, persona string, k Kind, userID, content string, importance float64, tags string) (int64, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, errors.New("empty memory")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM memories WHERE user_id=? AND persona=? AND lower(content)=lower(?)`, userID, persona, content).Scan(&id)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE memories SET score=min(score+0.3,3), importance=max(importance,?), accessed=? WHERE id=?`, importance, now(), id)
		return id, err
	}
	var blob []byte
	if s.emb != nil {
		if v, e := s.emb.Embed(ctx, content); e == nil {
			blob = encodeVec(v)
		}
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO memories(persona,kind,content,user_id,importance,tags,embedding,created,accessed) VALUES(?,?,?,?,?,?,?,?,?)`,
		persona, string(k), content, userID, importance, tags, blob, now(), now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]{3,}`)

// ftsQuery turns free text into a safe OR query (no FTS operators leak in).
func ftsQuery(q string) string {
	seen := map[string]bool{}
	var parts []string
	for _, w := range wordRe.FindAllString(strings.ToLower(q), -1) {
		if !seen[w] && len(parts) < 12 {
			seen[w] = true
			parts = append(parts, `"`+w+`"`)
		}
	}
	return strings.Join(parts, " OR ")
}

// Recall returns memories visible to userID (their own plus global).
func (s *Store) Recall(ctx context.Context, persona, userID, query string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 6
	}
	fq := ftsQuery(query)
	var rows *sql.Rows
	var err error
	if fq == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT id,persona,kind,content,user_id,importance,score,tags,created,accessed,access_count FROM memories
			WHERE (user_id=? OR user_id='') AND persona IN (?, '') AND kind!='introspective' ORDER BY importance*score DESC, accessed DESC LIMIT ?`, userID, persona, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT m.id,m.persona,m.kind,m.content,m.user_id,m.importance,m.score,m.tags,m.created,m.accessed,m.access_count
			FROM memories_fts f JOIN memories m ON m.id=f.rowid
			WHERE memories_fts MATCH ? AND (m.user_id=? OR m.user_id='') AND m.persona IN (?, '') AND m.kind!='introspective'
			ORDER BY bm25(memories_fts) - (m.importance*m.score) LIMIT ?`, fq, userID, persona, limit*4)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		var k string
		if err := rows.Scan(&m.ID, &m.Persona, &k, &m.Content, &m.UserID, &m.Importance, &m.Score, &m.Tags, &m.Created, &m.Accessed, &m.Count); err != nil {
			return nil, err
		}
		m.Kind = Kind(k)
		out = append(out, m)
	}
	if s.emb != nil && len(out) > 1 && query != "" {
		out = s.rerank(ctx, out, query)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	for _, m := range out {
		_, _ = s.db.ExecContext(ctx, `UPDATE memories SET accessed=?, access_count=access_count+1 WHERE id=?`, now(), m.ID)
	}
	return out, rows.Err()
}

func (s *Store) rerank(ctx context.Context, ms []Memory, q string) []Memory {
	qv, err := s.emb.Embed(ctx, q)
	if err != nil {
		return ms
	}
	type sc struct {
		m Memory
		s float64
	}
	var xs []sc
	for _, m := range ms {
		var blob []byte
		_ = s.db.QueryRowContext(ctx, `SELECT embedding FROM memories WHERE id=?`, m.ID).Scan(&blob)
		xs = append(xs, sc{m, cosine(qv, decodeVec(blob))})
	}
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].s > xs[j].s })
	for i := range xs {
		ms[i] = xs[i].m
	}
	return ms
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return d / (math.Sqrt(na) * math.Sqrt(nb))
}

// Forget deletes one memory, only if it belongs to userID (or is global and
// userID is the empty owner scope).
func (s *Store) Forget(ctx context.Context, userID string, id int64) (bool, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// ForgetUser erases everything stored about a person (right to be forgotten).
func (s *Store) ForgetUser(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM memories WHERE user_id=?`, `DELETE FROM people WHERE user_id=?`,
		`DELETE FROM relationships WHERE user_id=?`, `DELETE FROM facts WHERE user_id=?`, `DELETE FROM turns WHERE user_id=?`, `DELETE FROM reminders WHERE user_id=?`,
		`DELETE FROM accounts WHERE person_id=?`, `DELETE FROM link_codes WHERE person_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Maintain decays unused memories and prunes the dead ones. Important
// memories decay slower and are never pruned below the floor.
func (s *Store) Maintain(ctx context.Context) (pruned int64, err error) {
	if _, err = s.db.ExecContext(ctx, `UPDATE memories SET score = score * (0.97 + 0.03*importance) WHERE kind!='semantic'`); err != nil {
		return
	}
	r, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE score < 0.15 AND importance < 0.7 AND access_count < 3`)
	if err != nil {
		return
	}
	return r.RowsAffected()
}

// ---- people, relationships, facts ----

type Relationship struct {
	UserID       string
	Name         string
	Persona      string
	Dynamic      string
	Familiarity  float64
	Interactions int
	InsideJokes  []string
}

// Touch records an interaction and nudges familiarity up with diminishing returns.
func (s *Store) Touch(ctx context.Context, persona, userID, name string) error {
	t := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO people(user_id,name,names,first_seen,last_seen) VALUES(?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET name=excluded.name, last_seen=excluded.last_seen`, userID, name, "[]", t, t); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO relationships(user_id,persona,familiarity,interactions,last_seen) VALUES(?,?,0.02,1,?)
		ON CONFLICT(user_id,persona) DO UPDATE SET interactions=interactions+1, familiarity=min(1, familiarity+0.01*(1-familiarity)), last_seen=excluded.last_seen`, userID, persona, t)
	return err
}

func (s *Store) Relationship(ctx context.Context, persona, userID string) (Relationship, error) {
	r := Relationship{UserID: userID, Persona: persona}
	var jokes string
	err := s.db.QueryRowContext(ctx, `SELECT r.dynamic,r.familiarity,r.interactions,r.inside_jokes,coalesce(p.name,'') FROM relationships r LEFT JOIN people p ON p.user_id=r.user_id WHERE r.user_id=? AND r.persona=?`,
		userID, persona).Scan(&r.Dynamic, &r.Familiarity, &r.Interactions, &jokes, &r.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	_ = json.Unmarshal([]byte(jokes), &r.InsideJokes)
	return r, err
}

func (s *Store) SetDynamic(ctx context.Context, persona, userID, dynamic string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE relationships SET dynamic=? WHERE user_id=? AND persona=?`, dynamic, userID, persona)
	return err
}

func (s *Store) AddInsideJoke(ctx context.Context, persona, userID, joke string) error {
	r, err := s.Relationship(ctx, persona, userID)
	if err != nil {
		return err
	}
	for _, j := range r.InsideJokes {
		if strings.EqualFold(j, joke) {
			return nil
		}
	}
	js := append(r.InsideJokes, joke)
	if len(js) > 8 {
		js = js[len(js)-8:]
	}
	b, _ := json.Marshal(js)
	_, err = s.db.ExecContext(ctx, `UPDATE relationships SET inside_jokes=? WHERE user_id=? AND persona=?`, string(b), userID, persona)
	return err
}

// Describe renders the relationship for the prompt.
func (r Relationship) Describe() string {
	if r.Interactions == 0 {
		return "You have not talked to this person before."
	}
	var b strings.Builder
	switch {
	case r.Familiarity > 0.6:
		b.WriteString("You know this person well.")
	case r.Familiarity > 0.2:
		b.WriteString("You have talked with this person a few times.")
	default:
		b.WriteString("You barely know this person.")
	}
	if r.Dynamic != "" {
		b.WriteString(" Dynamic: " + r.Dynamic)
	}
	if len(r.InsideJokes) > 0 {
		b.WriteString(" Inside jokes: " + strings.Join(r.InsideJokes, "; ") + ".")
	}
	return b.String()
}

func (s *Store) SetFact(ctx context.Context, userID, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO facts(user_id,key,value,updated) VALUES(?,?,?,?) ON CONFLICT(user_id,key) DO UPDATE SET value=excluded.value, updated=excluded.updated`, userID, key, value, now())
	return err
}

func (s *Store) Facts(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM facts WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ---- turns ----

func (s *Store) LogTurn(ctx context.Context, persona, userID, channelID, userMsg, reply string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO turns(persona,user_id,channel_id,user_msg,reply,ts) VALUES(?,?,?,?,?,?)`, persona, userID, channelID, userMsg, reply, now())
	return err
}

type Turn struct{ UserID, UserMsg, Reply, TS string }

// RecentTurns returns the last n turns of a channel, oldest first.
func (s *Store) RecentTurns(ctx context.Context, channelID string, n int) ([]Turn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id,user_msg,reply,ts FROM turns WHERE channel_id=? ORDER BY id DESC LIMIT ?`, channelID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		var t Turn
		if err := rows.Scan(&t.UserID, &t.UserMsg, &t.Reply, &t.TS); err != nil {
			return nil, err
		}
		out = append([]Turn{t}, out...)
	}
	return out, rows.Err()
}

// Stats for the panel.
func (s *Store) Stats(ctx context.Context) map[string]int {
	out := map[string]int{}
	for _, t := range []string{"memories", "people", "relationships", "turns"} {
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+t).Scan(&n)
		out[t] = n
	}
	return out
}

// ---- reminders ----

type Reminder struct {
	ID        int64
	UserID    string
	ChannelID string
	Content   string
	Due       time.Time
}

func (s *Store) AddReminder(ctx context.Context, userID, channelID, content string, due time.Time) (int64, error) {
	r, err := s.db.ExecContext(ctx, `INSERT INTO reminders(user_id,channel_id,content,due) VALUES(?,?,?,?)`, userID, channelID, content, due.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// DueReminders returns and marks done every reminder due by now.
func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,channel_id,content,due FROM reminders WHERE done=0 AND due<=? ORDER BY due`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	var out []Reminder
	for rows.Next() {
		var r Reminder
		var due string
		if err := rows.Scan(&r.ID, &r.UserID, &r.ChannelID, &r.Content, &due); err != nil {
			rows.Close()
			return nil, err
		}
		r.Due, _ = time.Parse(time.RFC3339, due)
		out = append(out, r)
	}
	rows.Close()
	for _, r := range out {
		if _, err := s.db.ExecContext(ctx, `UPDATE reminders SET done=1 WHERE id=?`, r.ID); err != nil {
			return out, err
		}
	}
	return out, nil
}
