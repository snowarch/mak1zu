package memory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// What the night shift leaves behind: a private diary entry per person per
// night, and a short list of things she means to bring up next time ("unsaid").
// Both are per person, readable and deletable by that person, and both expire:
// she is meant to be thinking about you, not to be an archive.

const nightSchema = `
CREATE TABLE IF NOT EXISTS diary(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id TEXT NOT NULL,
  persona TEXT NOT NULL DEFAULT '',
  day TEXT NOT NULL,
  text TEXT NOT NULL,
  created TEXT NOT NULL,
  UNIQUE(person_id, persona, day)
);
CREATE TABLE IF NOT EXISTS unsaid(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id TEXT NOT NULL,
  persona TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL,
  created TEXT NOT NULL,
  expires TEXT NOT NULL,
  said TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_unsaid_person ON unsaid(person_id, said);
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

const maxUnsaid = 3

// Meta is a tiny key/value place for bookkeeping (when the night last ran).
func (s *Store) Meta(ctx context.Context, key string) string {
	var v string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ActivePeople are the people she actually talked with since a time, busiest
// first. Someone who said one thing is not worth a night's reflection.
func (s *Store) ActivePeople(ctx context.Context, since time.Time, minTurns, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM turns WHERE ts>=? AND user_id IN (SELECT user_id FROM people)
		GROUP BY user_id HAVING count(*)>=? ORDER BY count(*) DESC LIMIT ?`, since.UTC().Format(time.RFC3339), minTurns, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TurnsSince is one person's conversations since a time, oldest first.
func (s *Store) TurnsSince(ctx context.Context, personID string, since time.Time, limit int) ([]Turn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id,user_msg,reply,ts FROM (SELECT id,user_id,user_msg,reply,ts FROM turns WHERE user_id=? AND ts>=? ORDER BY id DESC LIMIT ?) ORDER BY id`,
		personID, since.UTC().Format(time.RFC3339), limit)
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
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListMemories reads a person's own memories without counting as a recall
// (reading them to tidy them must not make them look important).
func (s *Store) ListMemories(ctx context.Context, persona, personID string, limit int) ([]Memory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,persona,kind,content,user_id,importance,score,tags,source,created,accessed,access_count FROM memories
		WHERE user_id=? AND persona IN (?, '') AND kind!='introspective' ORDER BY id DESC LIMIT ?`, personID, persona, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		var k string
		if err := rows.Scan(&m.ID, &m.Persona, &k, &m.Content, &m.UserID, &m.Importance, &m.Score, &m.Tags, &m.Source, &m.Created, &m.Accessed, &m.Count); err != nil {
			return nil, err
		}
		m.Kind = Kind(k)
		out = append(out, m)
	}
	return out, rows.Err()
}

// DiaryEntry is what she wrote about a night with one person.
type DiaryEntry struct {
	ID   int64
	Day  string
	Text string
}

// AddDiary stores the night's entry; writing the same night again replaces it.
func (s *Store) AddDiary(ctx context.Context, persona, personID, day, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("empty diary entry")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO diary(person_id,persona,day,text,created) VALUES(?,?,?,?,?)
		ON CONFLICT(person_id,persona,day) DO UPDATE SET text=excluded.text, created=excluded.created`, personID, persona, day, text, now())
	return err
}

// Diary lists the newest entries about a person.
func (s *Store) Diary(ctx context.Context, persona, personID string, limit int) ([]DiaryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,day,text FROM diary WHERE person_id=? AND persona=? ORDER BY day DESC LIMIT ?`, personID, persona, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiaryEntry
	for rows.Next() {
		var d DiaryEntry
		if err := rows.Scan(&d.ID, &d.Day, &d.Text); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ClearDiary deletes everything she wrote about this person.
func (s *Store) ClearDiary(ctx context.Context, personID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM diary WHERE person_id=?`, personID)
	return err
}

// Unsaid is something she means to bring up the next time they talk.
type Unsaid struct {
	ID      int64
	Text    string
	Created string
}

// AddUnsaid queues something to say. At most three wait at once (the newest
// win), the same text is not queued twice, and each one expires.
func (s *Store) AddUnsaid(ctx context.Context, persona, personID, text string, ttl time.Duration) error {
	text = clean(text, 200)
	if text == "" {
		return errors.New("empty")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM unsaid WHERE person_id=? AND persona=? AND said='' AND lower(text)=lower(?)`, personID, persona, text).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO unsaid(person_id,persona,text,created,expires) VALUES(?,?,?,?,?)`,
		personID, persona, text, now(), time.Now().Add(ttl).UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM unsaid WHERE person_id=? AND persona=? AND said='' AND id NOT IN
		(SELECT id FROM unsaid WHERE person_id=? AND persona=? AND said='' ORDER BY id DESC LIMIT ?)`, personID, persona, personID, persona, maxUnsaid)
	return err
}

// PendingUnsaid is what she still means to say and has not expired.
func (s *Store) PendingUnsaid(ctx context.Context, persona, personID string, at time.Time) ([]Unsaid, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,created FROM unsaid WHERE person_id=? AND persona=? AND said='' AND expires>? ORDER BY id`,
		personID, persona, at.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Unsaid
	for rows.Next() {
		var u Unsaid
		if err := rows.Scan(&u.ID, &u.Text, &u.Created); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkSaid retires unsaid items once they were put in front of her for a
// reply: she had her chance, and she must not keep repeating them.
func (s *Store) MarkSaid(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE unsaid SET said=? WHERE id=?`, now(), id); err != nil {
			return err
		}
	}
	return nil
}
