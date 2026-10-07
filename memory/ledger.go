package memory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The ledger is what she keeps about one person beyond raw memories: things
// still in flight (threads) and shared references she may call back to (bits).
// Both are per person, so they follow someone across platforms, and both are
// small on purpose: a cap, a dedupe and a cooldown keep her from nagging.

const ledgerSchema = `
CREATE TABLE IF NOT EXISTS threads(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id TEXT NOT NULL,
  text TEXT NOT NULL,
  due TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open',
  source TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL,
  closed TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_thr_person ON threads(person_id, status);
CREATE TABLE IF NOT EXISTS bits(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  person_id TEXT NOT NULL,
  persona TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL,
  trigger TEXT NOT NULL DEFAULT '',
  origin TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL,
  last_used TEXT NOT NULL DEFAULT '',
  uses INTEGER NOT NULL DEFAULT 0,
  UNIQUE(person_id, persona, text)
);
`

const (
	maxOpenThreads = 12
	maxBits        = 12
	// BitCooldown is how long a running bit rests after she uses it.
	BitCooldown = 3 * 24 * time.Hour
)

// Thread is something in flight in someone's life: an exam on Thursday, a sick
// cat, a decision they were stuck on.
type Thread struct {
	ID      int64
	Text    string
	Due     string // RFC3339 or ""
	Source  string
	Created string
}

func clean(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

// AddThread opens a thread. The same open text is not opened twice; past the
// cap the oldest open thread is dropped, because a ledger nobody can scan is
// a nag list.
func (s *Store) AddThread(ctx context.Context, personID, source, text string, due time.Time) (int64, error) {
	text = clean(text, 200)
	if text == "" {
		return 0, errors.New("empty thread")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM threads WHERE person_id=? AND status='open' AND lower(text)=lower(?)`, personID, text).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	d := ""
	if !due.IsZero() {
		d = due.UTC().Format(time.RFC3339)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO threads(person_id,text,due,source,created) VALUES(?,?,?,?,?)`, personID, text, d, source, now())
	if err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE threads SET status='dropped', closed=? WHERE person_id=? AND status='open' AND id NOT IN
		(SELECT id FROM threads WHERE person_id=? AND status='open' ORDER BY id DESC LIMIT ?)`, now(), personID, personID, maxOpenThreads); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// OpenThreads lists what is still open: dated ones first, soonest first.
func (s *Store) OpenThreads(ctx context.Context, personID string, limit int) ([]Thread, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,due,source,created FROM threads WHERE person_id=? AND status='open'
		ORDER BY due='' , due, id LIMIT ?`, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Thread
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.Text, &t.Due, &t.Source, &t.Created); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CloseThread closes one of this person's threads (never anyone else's).
func (s *Store) CloseThread(ctx context.Context, personID string, id int64) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE threads SET status='closed', closed=? WHERE id=? AND person_id=? AND status='open'`, now(), id, personID)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// Bit is a running reference between her and one person, with a phrase that
// signals she used it, so it can rest before it comes back.
type Bit struct {
	ID       int64
	Text     string
	Trigger  string
	Origin   string
	LastUsed string
	Uses     int
}

// AddBit records a shared reference. trigger is a short phrase that would
// appear in her reply when she calls the bit back; without one it cannot rest
// automatically, so it is only ever offered, never marked used.
func (s *Store) AddBit(ctx context.Context, persona, personID, origin, text, trigger string) (int64, error) {
	text, trigger = clean(text, 160), strings.ToLower(clean(trigger, 40))
	if text == "" {
		return 0, errors.New("empty bit")
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM bits WHERE person_id=? AND persona=? AND lower(text)=lower(?)`, personID, persona, text).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO bits(person_id,persona,text,trigger,origin,created) VALUES(?,?,?,?,?,?)`, personID, persona, text, trigger, origin, now())
	if err != nil {
		return 0, err
	}
	// Keep the best twelve: the most used, then the newest.
	_, err = s.db.ExecContext(ctx, `DELETE FROM bits WHERE person_id=? AND persona=? AND id NOT IN
		(SELECT id FROM bits WHERE person_id=? AND persona=? ORDER BY uses DESC, id DESC LIMIT ?)`, personID, persona, personID, persona, maxBits)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// BitsReady are the bits she may call back to now: the ones that have rested
// for the cooldown.
func (s *Store) BitsReady(ctx context.Context, persona, personID string, at time.Time, limit int) ([]Bit, error) {
	cut := at.Add(-BitCooldown).UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,trigger,origin,last_used,uses FROM bits
		WHERE person_id=? AND persona=? AND (last_used='' OR last_used<?) ORDER BY last_used, id LIMIT ?`, personID, persona, cut, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bit
	for rows.Next() {
		var b Bit
		if err := rows.Scan(&b.ID, &b.Text, &b.Trigger, &b.Origin, &b.LastUsed, &b.Uses); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// NoteBitUse puts to rest every bit whose trigger appears in her reply, and
// returns how many.
func (s *Store) NoteBitUse(ctx context.Context, persona, personID, reply string, at time.Time) (int, error) {
	reply = strings.ToLower(reply)
	rows, err := s.db.QueryContext(ctx, `SELECT id,trigger FROM bits WHERE person_id=? AND persona=? AND trigger!=''`, personID, persona)
	if err != nil {
		return 0, err
	}
	var hit []int64
	for rows.Next() {
		var id int64
		var tr string
		if err := rows.Scan(&id, &tr); err != nil {
			rows.Close()
			return 0, err
		}
		if strings.Contains(reply, tr) {
			hit = append(hit, id)
		}
	}
	rows.Close()
	for _, id := range hit {
		if _, err := s.db.ExecContext(ctx, `UPDATE bits SET last_used=?, uses=uses+1 WHERE id=?`, at.UTC().Format(time.RFC3339), id); err != nil {
			return 0, err
		}
	}
	return len(hit), nil
}

// Bits lists everything, for the person to read and prune.
func (s *Store) Bits(ctx context.Context, persona, personID string) ([]Bit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,trigger,origin,last_used,uses FROM bits WHERE person_id=? AND persona=? ORDER BY id`, personID, persona)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bit
	for rows.Next() {
		var b Bit
		if err := rows.Scan(&b.ID, &b.Text, &b.Trigger, &b.Origin, &b.LastUsed, &b.Uses); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DropBit removes one of this person's bits.
func (s *Store) DropBit(ctx context.Context, personID string, id int64) (bool, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM bits WHERE id=? AND person_id=?`, id, personID)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
