package hub

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"reflect"
)

// One append-only command log per project backs collaboration and lifecycle.
// The event log owns the ordered read, the idempotent-admission rule, and the
// transactional append, so the two pipelines cannot drift apart; kind-specific
// shape and semantic validation stay with each pipeline.
var (
	errLogIDConflict = errors.New("command id recorded under another command or actor")
	errLogHead       = errors.New("command head conflict or log limit")
)

type eventLog[C any, E any] struct {
	table    string
	max      int
	id       func(C) string
	expected func(C) int
	equal    func(C, C) bool
	command  func(E) C
	sequence func(E) int
	actor    func(E) string
	issuer   func(E) string
}

var reviewLog = eventLog[ReviewCommand, ReviewEvent]{
	table:    "readmit_hub_reviews",
	max:      maxReviews,
	id:       func(c ReviewCommand) string { return c.ID },
	expected: func(c ReviewCommand) int { return c.Expected },
	equal:    func(a, b ReviewCommand) bool { return a == b },
	command:  func(e ReviewEvent) ReviewCommand { return e.Command },
	sequence: func(e ReviewEvent) int { return e.Sequence },
	actor:    func(e ReviewEvent) string { return e.Actor },
	issuer:   func(e ReviewEvent) string { return e.Issuer },
}

var lifecycleLog = eventLog[LifecycleCommand, LifecycleEvent]{
	table:    "readmit_hub_lifecycle",
	max:      maxLifecycle,
	id:       func(c LifecycleCommand) string { return c.ID },
	expected: func(c LifecycleCommand) int { return c.Expected },
	equal:    func(a, b LifecycleCommand) bool { return reflect.DeepEqual(a, b) },
	command:  func(e LifecycleEvent) LifecycleCommand { return e.Command },
	sequence: func(e LifecycleEvent) int { return e.Sequence },
	actor:    func(e LifecycleEvent) string { return e.Actor },
	issuer:   func(e LifecycleEvent) string { return e.Issuer },
}

// read returns one project's events in sequence order, bounded and strictly decoded.
func (l eventLog[C, E]) read(ctx context.Context, db *sql.DB, project string) ([]E, error) {
	if db == nil {
		return nil, errAccess
	}
	rows, e := db.QueryContext(ctx, `SELECT document FROM `+l.table+` WHERE project=$1 ORDER BY sequence`, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return l.scan(rows)
}

// readAll returns every project's events, ordered the way a backup records them.
func (l eventLog[C, E]) readAll(ctx context.Context, db *sql.DB) ([]E, error) {
	rows, e := db.QueryContext(ctx, `SELECT document FROM `+l.table+` ORDER BY project COLLATE "C", sequence`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return l.scan(rows)
}

func (l eventLog[C, E]) scan(rows *sql.Rows) ([]E, error) {
	events := []E{}
	for rows.Next() {
		var data string
		var event E
		if e := rows.Scan(&data); e != nil {
			return nil, e
		}
		if json.Unmarshal([]byte(data), &event, json.RejectUnknownMembers(true)) != nil {
			return nil, ErrIntegrity
		}
		events = append(events, event)
		if len(events) > l.max {
			return nil, ErrLimit
		}
	}
	return events, rows.Err()
}

func (l eventLog[C, E]) total(ctx context.Context, db *sql.DB) (int, error) {
	var total int
	e := db.QueryRowContext(ctx, `SELECT count(*) FROM `+l.table).Scan(&total)
	return total, e
}

// admit applies the idempotent-admission rule: a command whose id is already
// recorded by the same actor with the same content replays its recorded event;
// any other reuse of the id, a stale expected head, or a log at its limit
// refuses. The caller holds the store lock, so events and total cannot change
// between admission and commit.
func (l eventLog[C, E]) admit(events []E, total int, c C, actor, issuer string) (E, bool, error) {
	for _, event := range events {
		if l.id(l.command(event)) == l.id(c) {
			if l.equal(l.command(event), c) && l.actor(event) == actor && l.issuer(event) == issuer {
				return event, true, nil
			}
			var zero E
			return zero, false, errLogIDConflict
		}
	}
	var zero E
	if l.expected(c) != len(events) || total >= l.max {
		return zero, false, errLogHead
	}
	return zero, false, nil
}

// commit appends one admitted event and records team activity in the same
// transaction, so a retry of the same command id is always safe.
func (l eventLog[C, E]) commit(ctx context.Context, db *sql.DB, project string, event E) error {
	data, e := json.Marshal(event)
	if e != nil {
		return e
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO `+l.table+`(project,sequence,id,document) VALUES($1,$2,$3,$4)`, project, l.sequence(event), l.id(l.command(event)), string(data)); e == nil {
		_, e = tx.ExecContext(ctx, `UPDATE readmit_hub_schema SET team_enabled=true WHERE singleton`)
	}
	if e == nil {
		e = tx.Commit()
	}
	return e
}
