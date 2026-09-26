package hub

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// The project log is the one module a project's review and lifecycle logs are
// recorded and replayed through. A live command is recorded for a principal:
// idempotent replay, the head check, removal, recipient eligibility, support
// derivations and the event's stamp, taken from the log's clock. A backed-up
// event is replayed through the same rules before a restore writes it. The
// log reads and appends through projectStorage, so its rules run the same over
// PostgreSQL and in memory.
//
// A request reads a project's lifecycle log once (open), and its review log
// once when it needs it (reviews); every decision in that request is made
// against those reads. The caller holds the store lock while it records, so
// neither log changes between a read and an append.

// Refusals a handler maps to a status. Admission refusals are errLogIDConflict
// and errLogHead; a review the protocol refuses is errReviewRefused joined with
// the protocol's own sentinel.
var (
	errLogUnavailable    = errors.New("project log unavailable")
	errCommitUnavailable = errors.New("project log commit unavailable")
	errSupportNeedsV2    = errors.New("history carrying support commands requires v2")
	errPolicyChanged     = errors.New("sharing policy changed")
	errPolicyUnavailable = errors.New("access policy unavailable")
	errRecipient         = errors.New("recipient refused")
	errReviewRefused     = errors.New("review refused")
	errRemoval           = errors.New("removal refused")
	errLifecycleConflict = errors.New("lifecycle conflict")
	errAuditUnavailable  = errors.New("audit unavailable")
	errAuditRetry        = errors.New("audit unavailable; retry same id")
)

// projectStorage is the seam under the project log.
type projectStorage interface {
	reviews(ctx context.Context, project string) ([]ReviewEvent, error)
	lifecycle(ctx context.Context, project string) ([]LifecycleEvent, error)
	// reviewTotal and lifecycleTotal count every project's events: each log's
	// limit is for the whole catalogue a backup retains.
	reviewTotal(ctx context.Context) (int, error)
	lifecycleTotal(ctx context.Context) (int, error)
	// appendReview and appendLifecycle append one admitted event and record
	// team activity with it.
	appendReview(ctx context.Context, event ReviewEvent) error
	appendLifecycle(ctx context.Context, event LifecycleEvent) error
	linked(ctx context.Context, project, digest string) (bool, error)
	artifact(ctx context.Context, digest string) ([]byte, error)
}

type projectLog struct {
	storage projectStorage
	now     func() time.Time
}

// projects is the store's project log, over PostgreSQL and the host's clock.
func (s *Store) projects() projectLog {
	return projectLog{storage: postgresStorage{s}, now: time.Now}
}

// project is what one read of a project's lifecycle log establishes.
type projectView struct {
	name      string
	events    []LifecycleEvent
	lifecycle hubprotocol.Lifecycle
}

func (l projectLog) open(ctx context.Context, name string) (projectView, error) {
	events, e := l.storage.lifecycle(ctx, name)
	if e != nil {
		return projectView{}, e
	}
	return projectView{name: name, events: events, lifecycle: hubprotocol.DeriveLifecycle(events)}, nil
}

// removed is the one removed-principal rule: policy reinstallation cannot
// resurrect a principal the project's log removed.
func (p projectView) removed(principal Principal) bool {
	return p.lifecycle.Removed(principal.Issuer, principal.Subject)
}

func (p projectView) retired(digest string) bool { return p.lifecycle.Retired(digest) }

func (p projectView) history() hubprotocol.LifecycleHistory {
	return hubprotocol.LifecycleHistory{Schema: hubprotocol.LifecycleHistorySchema, Head: len(p.events), Events: p.events, Tips: p.lifecycle.Tips(), Warning: hubprotocol.CustodyWarning}
}

// reviews is what one read of a project's review log establishes.
type reviews struct {
	events []ReviewEvent
	state  hubprotocol.Reviews
}

// reviews reads the project's review log once. A v1 route cannot read a log
// that carries support commands.
func (l projectLog) reviews(ctx context.Context, p projectView, v2 bool) (reviews, error) {
	events, e := l.storage.reviews(ctx, p.name)
	if e != nil {
		return reviews{}, errLogUnavailable
	}
	r := reviews{events: events, state: hubprotocol.DeriveReviews(events)}
	if !v2 && r.state.HasSupport() {
		return r, errSupportNeedsV2
	}
	return r, nil
}

// history answers a search of the review history, or of the principal's own
// notifications.
func (r reviews) history(principal Principal, notifications bool, query hubprotocol.ReviewQuery, v2 bool) hubprotocol.ReviewHistory {
	filtered := []ReviewEvent{}
	for _, event := range r.events {
		if event.Sequence <= query.After || (query.Evidence != "" && event.Command.Evidence != query.Evidence) || (notifications && (event.Command.Recipient != principal.Subject || event.Issuer != principal.Issuer)) || !strings.Contains(strings.ToLower(event.Command.Text), strings.ToLower(query.Text)) {
			continue
		}
		filtered = append(filtered, event)
	}
	return hubprotocol.ReviewHistory{Schema: r.state.HistorySchema(v2), Head: len(r.events), Events: filtered}
}

// accessPolicy yields the role the access policy grants a subject in the
// project. The log asks for it only where a rule needs it, after idempotent
// replay, so an unreadable policy never blocks answering a recorded command.
type accessPolicy func() (func(subject string) string, error)

// recordReview records a review command for a principal, or replays the
// event its id already recorded (true).
func (l projectLog) recordReview(ctx context.Context, p projectView, r reviews, principal Principal, c ReviewCommand, policy accessPolicy) (ReviewEvent, bool, error) {
	if hubprotocol.IsSupport(c) && !r.state.Current(c) {
		return ReviewEvent{}, false, errPolicyChanged
	}
	total, e := l.storage.reviewTotal(ctx)
	if e != nil {
		return ReviewEvent{}, false, errLogUnavailable
	}
	existing, replay, e := reviewLog.admit(r.events, total, c, principal.Subject, principal.Issuer)
	if e != nil || replay {
		return existing, replay, e
	}
	role, e := policy()
	if e != nil {
		return ReviewEvent{}, false, errPolicyUnavailable
	}
	if c.Recipient != "" {
		if hubprotocol.IsSupport(c) && p.lifecycle.Removed(principal.Issuer, c.Recipient) {
			return ReviewEvent{}, false, errRecipient
		}
		granted := role(c.Recipient)
		if granted == "" || granted == "runner" || ((c.Kind == "review-request" || hubprotocol.IsSupportRequest(c)) && (!roleAllows(granted, "approval") || c.Recipient == principal.Subject)) {
			return ReviewEvent{}, false, errRecipient
		}
	}
	load := l.linkedArtifact(ctx, p.name)
	if hubprotocol.IsSupport(c) {
		load = l.supportArtifact(ctx, p)
	}
	if e = r.state.Validate(c, principal.Subject, principal.Issuer, load); e != nil {
		return ReviewEvent{}, false, errors.Join(errReviewRefused, hubSentinel(e))
	}
	schema := hubprotocol.ReviewEventV1
	if hubprotocol.IsSupport(c) {
		schema = hubprotocol.ReviewEventV2
	}
	event := ReviewEvent{Schema: schema, Project: p.name, Sequence: len(r.events) + 1, Issuer: principal.Issuer, Actor: principal.Subject, At: l.now().UTC().Format(time.RFC3339Nano), Command: c}
	if e = l.storage.appendReview(ctx, event); e != nil {
		return ReviewEvent{}, false, errCommitUnavailable
	}
	return event, false, nil
}

// approvedSupport is the exact support content the project's review log
// approved for release under digest.
func (l projectLog) approvedSupport(ctx context.Context, p projectView, digest string) ([]byte, error) {
	r, e := l.reviews(ctx, p, true)
	if e != nil {
		return nil, e
	}
	return r.state.Approved(digest, l.supportArtifact(ctx, p))
}

// lifecycleRecord is what recording a lifecycle command established: the
// event, the log with it, whether the event was already recorded, and, for an
// audit-export recorded now, the review log it stamped the head of, so its
// export needs no second read.
type lifecycleRecord struct {
	event       LifecycleEvent
	events      []LifecycleEvent
	replayed    bool
	reviews     []ReviewEvent
	reviewsRead bool
}

// recordLifecycle records a lifecycle command for a principal, or replays the
// event its id already recorded.
func (l projectLog) recordLifecycle(ctx context.Context, p projectView, principal Principal, c LifecycleCommand, policy accessPolicy) (lifecycleRecord, error) {
	total, e := l.storage.lifecycleTotal(ctx)
	if e != nil {
		return lifecycleRecord{}, errLogUnavailable
	}
	existing, replay, e := lifecycleLog.admit(p.events, total, c, principal.Subject, principal.Issuer)
	if e != nil || replay {
		return lifecycleRecord{event: existing, events: p.events, replayed: replay}, e
	}
	if c.Kind == "remove-user" {
		if removesActor(c, principal.Subject) {
			return lifecycleRecord{}, errRemoval
		}
		role, e := policy()
		if e != nil {
			return lifecycleRecord{}, errRemoval
		}
		if granted := role(c.Subject); granted == "" || granted == "owner" {
			return lifecycleRecord{}, errRemoval
		}
	}
	now := l.now().UTC()
	if e = p.lifecycle.Validate(c, l.linkedArtifact(ctx, p.name), now); e != nil {
		return lifecycleRecord{}, errLifecycleConflict
	}
	record := lifecycleRecord{event: LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: p.name, Sequence: len(p.events) + 1, Issuer: principal.Issuer, Actor: principal.Subject, At: now.Format(time.RFC3339Nano), Command: c}}
	if c.Kind == "audit-export" {
		if record.reviews, e = l.storage.reviews(ctx, p.name); e != nil {
			return lifecycleRecord{}, errAuditUnavailable
		}
		record.reviewsRead = true
		record.event.ReviewHead = len(record.reviews)
	}
	if e = l.storage.appendLifecycle(ctx, record.event); e != nil {
		return lifecycleRecord{}, errCommitUnavailable
	}
	record.events = append(p.events, record.event)
	return record, nil
}

// auditExport is an audit-export event's export: both history prefixes the
// event recorded. A replayed event reads the review log again, so a retry
// answers the same export.
func (l projectLog) auditExport(ctx context.Context, record lifecycleRecord) (hubprotocol.AuditExport, error) {
	event, recorded := record.event, record.reviews
	if !record.reviewsRead {
		var e error
		if recorded, e = l.storage.reviews(ctx, event.Project); e != nil {
			return hubprotocol.AuditExport{}, errAuditRetry
		}
	}
	if event.ReviewHead > len(recorded) {
		return hubprotocol.AuditExport{}, errAuditUnavailable
	}
	recorded = recorded[:event.ReviewHead]
	return hubprotocol.AuditExport{Schema: hubprotocol.DeriveReviews(recorded).AuditSchema(), Project: event.Project, Lifecycle: record.events[:event.Sequence], ReviewHead: len(recorded), Reviews: recorded, Warning: hubprotocol.CustodyWarning}, nil
}

// removesActor is the rule that no principal removes themself.
func removesActor(c LifecycleCommand, actor string) bool {
	return c.Kind == "remove-user" && c.Subject == actor
}

// linkedArtifact loads an artifact the project links.
func (l projectLog) linkedArtifact(ctx context.Context, project string) func(string) ([]byte, error) {
	return func(digest string) ([]byte, error) {
		if linked, e := l.storage.linked(ctx, project, digest); e != nil || !linked {
			return nil, ErrMissing
		}
		return l.storage.artifact(ctx, digest)
	}
}

// supportArtifact loads a linked artifact the project has not retired.
func (l projectLog) supportArtifact(ctx context.Context, p projectView) func(string) ([]byte, error) {
	linked := l.linkedArtifact(ctx, p.name)
	return func(digest string) ([]byte, error) {
		if p.retired(digest) {
			return nil, ErrMissing
		}
		return linked(digest)
	}
}

// replayReview decides a backed-up review event by the rules its live write
// was decided by, then appends it. The event's stamp must be the one a live
// write gives it. Rules that read the lifecycle log or the access policy are
// not replayed: a backup does not order the two logs against each other, and
// it retains no policy.
func (l projectLog) replayReview(ctx context.Context, event ReviewEvent) error {
	events, e := l.storage.reviews(ctx, event.Project)
	if e != nil {
		return e
	}
	total, e := l.storage.reviewTotal(ctx)
	if e != nil {
		return e
	}
	if !stamped(event.Sequence, len(events), event.Issuer, event.Actor, event.At) {
		return ErrIntegrity
	}
	if _, replay, e := reviewLog.admit(events, total, event.Command, event.Actor, event.Issuer); e != nil || replay {
		return ErrIntegrity
	}
	state := hubprotocol.DeriveReviews(events)
	if hubprotocol.IsSupport(event.Command) && !state.Current(event.Command) {
		return ErrIntegrity
	}
	if e = state.Validate(event.Command, event.Actor, event.Issuer, l.linkedArtifact(ctx, event.Project)); e != nil {
		return hubSentinel(e)
	}
	return l.storage.appendReview(ctx, event)
}

// replayLifecycle decides a backed-up lifecycle event by the rules its live
// write was decided by, at the instant it records, then appends it. The
// access policy's part of a removal is not replayed; a backup retains none.
func (l projectLog) replayLifecycle(ctx context.Context, event LifecycleEvent) error {
	p, e := l.open(ctx, event.Project)
	if e != nil {
		return e
	}
	recorded, e := l.storage.reviews(ctx, event.Project)
	if e != nil {
		return e
	}
	total, e := l.storage.lifecycleTotal(ctx)
	if e != nil {
		return e
	}
	if event.ReviewHead < 0 || event.ReviewHead > len(recorded) || (event.Command.Kind != "audit-export" && event.ReviewHead != 0) {
		return ErrIntegrity
	}
	if event.Schema != hubprotocol.LifecycleEventSchema || !stamped(event.Sequence, len(p.events), event.Issuer, event.Actor, event.At) {
		return ErrIntegrity
	}
	if _, replay, e := lifecycleLog.admit(p.events, total, event.Command, event.Actor, event.Issuer); e != nil || replay {
		return ErrIntegrity
	}
	if p.removed(Principal{Issuer: event.Issuer, Subject: event.Actor}) || removesActor(event.Command, event.Actor) {
		return ErrIntegrity
	}
	at, _ := time.Parse(time.RFC3339Nano, event.At)
	if e = p.lifecycle.Validate(event.Command, l.linkedArtifact(ctx, event.Project), at); e != nil {
		return hubSentinel(e)
	}
	return l.storage.appendLifecycle(ctx, event)
}

// stamped reports whether an event carries the stamp a live write gives the
// next event of a log holding head events: the next sequence, the author's
// bounded issuer and subject, and an instant.
func stamped(sequence, head int, issuer, actor, at string) bool {
	if sequence != head+1 || issuer == "" || !reviewText(issuer, 2048) || actor == "" || !reviewText(actor, 256) {
		return false
	}
	_, e := time.Parse(time.RFC3339Nano, at)
	return e == nil
}

// postgresStorage is the project log's storage in the hub's database.
type postgresStorage struct{ s *Store }

func (p postgresStorage) reviews(ctx context.Context, project string) ([]ReviewEvent, error) {
	return reviewLog.read(ctx, p.s.db, project)
}
func (p postgresStorage) lifecycle(ctx context.Context, project string) ([]LifecycleEvent, error) {
	return lifecycleLog.read(ctx, p.s.db, project)
}
func (p postgresStorage) reviewTotal(ctx context.Context) (int, error) {
	return reviewLog.total(ctx, p.s.db)
}
func (p postgresStorage) lifecycleTotal(ctx context.Context) (int, error) {
	return lifecycleLog.total(ctx, p.s.db)
}
func (p postgresStorage) appendReview(ctx context.Context, event ReviewEvent) error {
	return reviewLog.commit(ctx, p.s.db, event.Project, event)
}
func (p postgresStorage) appendLifecycle(ctx context.Context, event LifecycleEvent) error {
	return lifecycleLog.commit(ctx, p.s.db, event.Project, event)
}
func (p postgresStorage) linked(ctx context.Context, project, digest string) (bool, error) {
	return p.s.linkedProjectArtifact(ctx, project, digest)
}
func (p postgresStorage) artifact(ctx context.Context, digest string) ([]byte, error) {
	return p.s.Get(ctx, digest)
}

// memoryStorage is the project log's storage held in memory: what a restore
// replays a backup into before it writes anything, and what the log's rules
// are tested over.
type memoryStorage struct {
	reviewLogs     map[string][]ReviewEvent
	lifecycleLogs  map[string][]LifecycleEvent
	reviewCount    int
	lifecycleCount int
	links          map[projectLink]bool
	load           func(digest string) ([]byte, error)
}

// newMemoryStorage holds the given project links, reading linked artifacts
// through load.
func newMemoryStorage(links []projectLink, load func(digest string) ([]byte, error)) *memoryStorage {
	m := &memoryStorage{reviewLogs: map[string][]ReviewEvent{}, lifecycleLogs: map[string][]LifecycleEvent{}, links: map[projectLink]bool{}, load: load}
	for _, link := range links {
		m.links[link] = true
	}
	return m
}

func (m *memoryStorage) reviews(_ context.Context, project string) ([]ReviewEvent, error) {
	return m.reviewLogs[project][:len(m.reviewLogs[project]):len(m.reviewLogs[project])], nil
}
func (m *memoryStorage) lifecycle(_ context.Context, project string) ([]LifecycleEvent, error) {
	return m.lifecycleLogs[project][:len(m.lifecycleLogs[project]):len(m.lifecycleLogs[project])], nil
}
func (m *memoryStorage) reviewTotal(context.Context) (int, error)    { return m.reviewCount, nil }
func (m *memoryStorage) lifecycleTotal(context.Context) (int, error) { return m.lifecycleCount, nil }
func (m *memoryStorage) appendReview(_ context.Context, event ReviewEvent) error {
	m.reviewLogs[event.Project] = append(m.reviewLogs[event.Project], event)
	m.reviewCount++
	return nil
}
func (m *memoryStorage) appendLifecycle(_ context.Context, event LifecycleEvent) error {
	m.lifecycleLogs[event.Project] = append(m.lifecycleLogs[event.Project], event)
	m.lifecycleCount++
	return nil
}
func (m *memoryStorage) linked(_ context.Context, project, digest string) (bool, error) {
	return m.links[projectLink{Project: project, Digest: digest}], nil
}
func (m *memoryStorage) artifact(_ context.Context, digest string) ([]byte, error) {
	return m.load(digest)
}
