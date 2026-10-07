package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// A person is one human, however many places she meets them. Memory is keyed
// by person id; a platform account (a Discord user, the terminal, the web chat)
// is only a way to reach that person. The column is still called user_id in
// the other tables: it holds a person id.

const RoleOwner = "owner"

// Person is who someone is to her, independent of any platform.
type Person struct {
	ID        string
	Name      string // the platform display name last seen
	CallMe    string // what they asked to be called; wins over Name
	Pronouns  string
	Language  string
	TZ        string
	Role      string // "" or RoleOwner
	Checkins  string // "off" when they asked her not to start conversations
	Quiet     string // "23:00-08:00": hours she must not message them
	FirstSeen string
	LastSeen  string
}

// Display is the name she uses for them.
func (p Person) Display() string {
	if p.CallMe != "" {
		return p.CallMe
	}
	return p.Name
}

func (p Person) IsOwner() bool { return p.Role == RoleOwner }

const personSchema = `
CREATE TABLE IF NOT EXISTS accounts(
  transport TEXT NOT NULL,
  external_id TEXT NOT NULL,
  person_id TEXT NOT NULL,
  display TEXT NOT NULL DEFAULT '',
  linked TEXT NOT NULL,
  PRIMARY KEY(transport, external_id)
);
CREATE INDEX IF NOT EXISTS idx_acc_person ON accounts(person_id);
CREATE TABLE IF NOT EXISTS link_codes(
  code TEXT PRIMARY KEY,
  person_id TEXT NOT NULL,
  expires TEXT NOT NULL
);
`

// migrate adds columns that databases created by older versions lack. Old
// rows keep their ids: a legacy user id simply becomes a person id.
func migrate(db *sql.DB) error {
	for table, cols := range map[string][]string{
		"people":   {"call_me", "pronouns", "language", "tz", "role", "checkins", "quiet"},
		"memories": {"source"},
	} {
		have := map[string]bool{}
		rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				rows.Close()
				return err
			}
			have[name] = true
		}
		rows.Close()
		for _, c := range cols {
			if !have[c] {
				if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + c + ` TEXT NOT NULL DEFAULT ''`); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

const personCols = `user_id,name,call_me,pronouns,language,tz,role,checkins,quiet,first_seen,last_seen`

func scanPerson(r interface{ Scan(...any) error }) (Person, error) {
	var p Person
	err := r.Scan(&p.ID, &p.Name, &p.CallMe, &p.Pronouns, &p.Language, &p.TZ, &p.Role, &p.Checkins, &p.Quiet, &p.FirstSeen, &p.LastSeen)
	return p, err
}

// Person returns one person by id; ok is false when there is none.
func (s *Store) Person(ctx context.Context, id string) (Person, bool, error) {
	p, err := scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE user_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Person{}, false, nil
	}
	return p, err == nil, err
}

// Persons lists everyone she knows, most recently seen first.
func (s *Store) Persons(ctx context.Context) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+personCols+` FROM people ORDER BY last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func newPersonID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "p" + hex.EncodeToString(b)
}

// Resolve maps a platform account to its person, creating both on first
// sight. A person row that predates accounts (its id is the platform id) is
// adopted instead of duplicated, so upgrading loses nothing.
func (s *Store) Resolve(ctx context.Context, transport, external, display string) (Person, error) {
	if transport == "" || external == "" {
		return Person{}, errors.New("memory: resolve needs a transport and an external id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Person{}, err
	}
	defer tx.Rollback()
	t := now()

	var id string
	err = tx.QueryRowContext(ctx, `SELECT person_id FROM accounts WHERE transport=? AND external_id=?`, transport, external).Scan(&id)
	switch {
	case err == nil:
		if display != "" {
			_, _ = tx.ExecContext(ctx, `UPDATE accounts SET display=? WHERE transport=? AND external_id=?`, display, transport, external)
		}
	case errors.Is(err, sql.ErrNoRows):
		var legacy int
		_ = tx.QueryRowContext(ctx, `SELECT count(*) FROM people WHERE user_id=? AND NOT EXISTS (SELECT 1 FROM accounts WHERE person_id=?)`, external, external).Scan(&legacy)
		if legacy > 0 {
			id = external
		} else {
			id = newPersonID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO people(user_id,name,names,first_seen,last_seen) VALUES(?,?,?,?,?)`, id, display, "[]", t, t); err != nil {
				return Person{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO accounts(transport,external_id,person_id,display,linked) VALUES(?,?,?,?,?)`, transport, external, id, display, t); err != nil {
			return Person{}, err
		}
	default:
		return Person{}, err
	}
	p, err := scanPerson(tx.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE user_id=?`, id))
	if err != nil {
		return Person{}, err
	}
	return p, tx.Commit()
}

// Lookup finds the person behind an account without creating anything.
func (s *Store) Lookup(ctx context.Context, transport, external string) (Person, bool, error) {
	p, err := scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE user_id=(SELECT person_id FROM accounts WHERE transport=? AND external_id=?)`, transport, external))
	if errors.Is(err, sql.ErrNoRows) {
		return Person{}, false, nil
	}
	return p, err == nil, err
}

// ClaimOwner gives a person the owner role. Owner is a role on a person, not a
// platform key: it holds from every transport that person is linked to.
func (s *Store) ClaimOwner(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE people SET role=? WHERE user_id=?`, RoleOwner, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return errors.New("memory: no such person")
	}
	return nil
}

// HasOwner reports whether anyone holds the owner role.
func (s *Store) HasOwner(ctx context.Context) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM people WHERE role=?`, RoleOwner).Scan(&n)
	return n > 0
}

var quietRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$`)

// WantsCheckins is false once they asked her to leave conversations to them.
func (p Person) WantsCheckins() bool { return p.Checkins != "off" }

// InQuietHours reports whether t falls inside their quiet window, in their
// time zone when they gave one, otherwise the machine's.
func (p Person) InQuietHours(t time.Time) bool {
	if p.Quiet == "" {
		return false
	}
	if p.TZ != "" {
		if loc, err := time.LoadLocation(p.TZ); err == nil {
			t = t.In(loc)
		}
	}
	var h1, m1, h2, m2 int
	if _, err := fmt.Sscanf(p.Quiet, "%d:%d-%d:%d", &h1, &m1, &h2, &m2); err != nil {
		return false
	}
	cur, from, to := t.Hour()*60+t.Minute(), h1*60+m1, h2*60+m2
	if from <= to {
		return cur >= from && cur < to
	}
	return cur >= from || cur < to // the window crosses midnight
}

// Profile keys a person may set about themselves.
var profileKeys = map[string]int{"call_me": 40, "pronouns": 30, "language": 30, "tz": 40, "checkins": 3, "quiet": 11}

// SetProfile changes what she knows about how to treat this person. Values
// are pinned into her prompt, so they are single-line, short and free of
// control characters; an empty value clears the field.
func (s *Store) SetProfile(ctx context.Context, id, key, value string) error {
	max, ok := profileKeys[key]
	if !ok {
		return fmt.Errorf("unknown profile field %q", key)
	}
	value = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)), " ")
	if len([]rune(value)) > max {
		return fmt.Errorf("%s is too long (max %d characters)", key, max)
	}
	if key == "tz" && value != "" {
		if _, err := time.LoadLocation(value); err != nil {
			return fmt.Errorf("%q is not a time zone name like Europe/Madrid", value)
		}
	}
	switch key {
	case "checkins":
		if value != "" && value != "on" && value != "off" {
			return errors.New("checkins is on or off")
		}
	case "quiet":
		if value != "" && !quietRe.MatchString(value) {
			return errors.New("quiet hours look like 23:00-08:00")
		}
	}
	r, err := s.db.ExecContext(ctx, `UPDATE people SET `+key+`=? WHERE user_id=?`, value, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return errors.New("memory: no such person")
	}
	return nil
}

// Account is one way to reach a person.
type Account struct {
	Transport, ExternalID, Display, Linked string
}

func (s *Store) Accounts(ctx context.Context, personID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT transport,external_id,display,linked FROM accounts WHERE person_id=? ORDER BY linked`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.Transport, &a.ExternalID, &a.Display, &a.Linked); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- link codes ----

const (
	linkAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
	linkTTL      = 10 * time.Minute
)

// ErrBadCode is the one answer for a wrong, expired or already used code.
var ErrBadCode = errors.New("that code is wrong or expired")

// ErrTooMany stops guessing: a code is short on purpose, so attempts are capped.
var ErrTooMany = errors.New("too many wrong codes, wait a few minutes")

var (
	failMu    sync.Mutex
	failTimes []time.Time
)

func tooManyFails() bool {
	failMu.Lock()
	defer failMu.Unlock()
	keep := failTimes[:0]
	for _, t := range failTimes {
		if time.Since(t) < linkTTL {
			keep = append(keep, t)
		}
	}
	failTimes = keep
	return len(failTimes) >= 8
}

func noteFail() {
	failMu.Lock()
	failTimes = append(failTimes, time.Now())
	failMu.Unlock()
}

// NewLinkCode makes a single-use code that lets another account join this
// person. A new code replaces any earlier one for the same person.
func (s *Store) NewLinkCode(ctx context.Context, personID string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i := range code {
		code[i] = linkAlphabet[int(b[i])%len(linkAlphabet)]
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM link_codes WHERE person_id=? OR expires<?`, personID, now()); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO link_codes(code,person_id,expires) VALUES(?,?,?)`, string(code), personID, time.Now().Add(linkTTL).UTC().Format(time.RFC3339)); err != nil {
		return "", err
	}
	return string(code), tx.Commit()
}

// Link joins the account to the person who issued the code. If the account
// already belonged to another person, the two are merged: everything she knows
// about both becomes one memory.
func (s *Store) Link(ctx context.Context, transport, external, display, code string) (Person, error) {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if tooManyFails() {
		return Person{}, ErrTooMany
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Person{}, err
	}
	defer tx.Rollback()

	var target, expires string
	err = tx.QueryRowContext(ctx, `SELECT person_id,expires FROM link_codes WHERE code=?`, code).Scan(&target, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		noteFail()
		return Person{}, ErrBadCode
	}
	if err != nil {
		return Person{}, err
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM link_codes WHERE code=?`, code)
	if exp, _ := time.Parse(time.RFC3339, expires); time.Now().After(exp) {
		_ = tx.Commit()
		noteFail()
		return Person{}, ErrBadCode
	}

	var src string
	err = tx.QueryRowContext(ctx, `SELECT person_id FROM accounts WHERE transport=? AND external_id=?`, transport, external).Scan(&src)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `INSERT INTO accounts(transport,external_id,person_id,display,linked) VALUES(?,?,?,?,?)`, transport, external, target, display, now()); err != nil {
			return Person{}, err
		}
	case err != nil:
		return Person{}, err
	case src != target:
		if err := mergePersons(ctx, tx, src, target); err != nil {
			return Person{}, err
		}
	}
	p, err := scanPerson(tx.QueryRowContext(ctx, `SELECT `+personCols+` FROM people WHERE user_id=?`, target))
	if err != nil {
		return Person{}, err
	}
	return p, tx.Commit()
}

// mergePersons folds src into dst. On conflicts dst wins; nothing is lost that
// dst did not already have.
func mergePersons(ctx context.Context, tx *sql.Tx, src, dst string) error {
	exec := func(q string, a ...any) error { _, err := tx.ExecContext(ctx, q, a...); return err }
	for _, q := range []string{
		`UPDATE accounts SET person_id=? WHERE person_id=?`,
		`UPDATE memories SET user_id=? WHERE user_id=?`,
		`UPDATE turns SET user_id=? WHERE user_id=?`,
		`UPDATE reminders SET user_id=? WHERE user_id=?`,
		`UPDATE OR IGNORE facts SET user_id=? WHERE user_id=?`,
		`UPDATE threads SET person_id=? WHERE person_id=?`,
		`UPDATE OR IGNORE bits SET person_id=? WHERE person_id=?`,
		`UPDATE unsaid SET person_id=? WHERE person_id=?`,
		`UPDATE OR IGNORE diary SET person_id=? WHERE person_id=?`,
	} {
		if err := exec(q, dst, src); err != nil {
			return err
		}
	}
	for _, q := range []string{`DELETE FROM facts WHERE user_id=?`} {
		if err := exec(q, src); err != nil {
			return err
		}
	}
	for _, q := range []string{`DELETE FROM bits WHERE person_id=?`, `DELETE FROM diary WHERE person_id=?`} { // what dst already had
		if err := exec(q, src); err != nil {
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT persona,dynamic,familiarity,interactions,last_seen FROM relationships WHERE user_id=?`, src)
	if err != nil {
		return err
	}
	type rel struct {
		persona, dynamic, last string
		fam                    float64
		n                      int
	}
	var rels []rel
	for rows.Next() {
		var r rel
		if err := rows.Scan(&r.persona, &r.dynamic, &r.fam, &r.n, &r.last); err != nil {
			rows.Close()
			return err
		}
		rels = append(rels, r)
	}
	rows.Close()
	for _, r := range rels {
		var dyn string
		var fam float64
		var n int
		err := tx.QueryRowContext(ctx, `SELECT dynamic,familiarity,interactions FROM relationships WHERE user_id=? AND persona=?`, dst, r.persona).Scan(&dyn, &fam, &n)
		if errors.Is(err, sql.ErrNoRows) {
			if err := exec(`UPDATE relationships SET user_id=? WHERE user_id=? AND persona=?`, dst, src, r.persona); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if dyn == "" {
			dyn = r.dynamic
		}
		if r.fam > fam {
			fam = r.fam
		}
		if err := exec(`UPDATE relationships SET dynamic=?,familiarity=?,interactions=?,last_seen=max(last_seen,?) WHERE user_id=? AND persona=?`,
			dyn, fam, n+r.n, r.last, dst, r.persona); err != nil {
			return err
		}
		if err := exec(`DELETE FROM relationships WHERE user_id=? AND persona=?`, src, r.persona); err != nil {
			return err
		}
	}

	// Profile: dst's choices stay, gaps are filled from src, owner is never lost.
	if err := exec(`UPDATE people SET
		call_me=CASE WHEN call_me='' THEN (SELECT call_me FROM people WHERE user_id=?) ELSE call_me END,
		pronouns=CASE WHEN pronouns='' THEN (SELECT pronouns FROM people WHERE user_id=?) ELSE pronouns END,
		language=CASE WHEN language='' THEN (SELECT language FROM people WHERE user_id=?) ELSE language END,
		tz=CASE WHEN tz='' THEN (SELECT tz FROM people WHERE user_id=?) ELSE tz END,
		role=CASE WHEN role='' THEN (SELECT role FROM people WHERE user_id=?) ELSE role END,
		checkins=CASE WHEN checkins='' OR (checkins='on' AND (SELECT checkins FROM people WHERE user_id=?)='off') THEN (SELECT checkins FROM people WHERE user_id=?) ELSE checkins END,
		quiet=CASE WHEN quiet='' THEN (SELECT quiet FROM people WHERE user_id=?) ELSE quiet END,
		first_seen=min(first_seen,(SELECT first_seen FROM people WHERE user_id=?))
		WHERE user_id=?`, src, src, src, src, src, src, src, src, src, dst); err != nil {
		return err
	}
	return exec(`DELETE FROM people WHERE user_id=?`, src)
}
