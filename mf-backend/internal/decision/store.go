package decision

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows means the addressed conversation does not exist, or belongs to
// somebody else. The two are deliberately the same error: distinguishing them
// would turn this endpoint into an oracle for which conversation ids exist.
var ErrNoRows = errors.New("no rows")

// productPersona labels the threads this package owns. The column exists so a
// second surface can share the table; nothing else writes to it yet.
const productPersona = "persona"

// titleMaxRunes bounds a derived or user-supplied thread title. Long enough for
// the subject lines people actually type ("Türkiye'de hızlı market teslimatı
// pazarı"), short enough that the list never has to truncate mid-render.
const titleMaxRunes = 80

// Store owns the SQL for persona conversation history.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// ConversationSummary is one row of the history list.
//
// No message bodies: the list renders a title, a decision badge and a time, and
// a transcript can run to tens of kilobytes of evidence-laden replies. Fetching
// them here would make opening the sidebar cost more than running a turn.
type ConversationSummary struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Verdict      *string    `json:"verdict"`
	VerdictScore *int       `json:"verdict_score"`
	Turns        int        `json:"turns"`
	LastTurnAt   time.Time  `json:"last_turn_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// Message is one stored turn, with the research that produced it when the turn
// was the persona's.
type Message struct {
	Role     string         `json:"role"`
	Content  string         `json:"content"`
	Sources  []Source       `json:"sources"`
	Research []ResearchStep `json:"research"`
	Model    string         `json:"model"`
}

// Conversation is a thread with its full transcript, for resuming it.
type Conversation struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Verdict      *string   `json:"verdict"`
	VerdictScore *int      `json:"verdict_score"`
	Messages     []Message `json:"messages"`
	LastTurnAt   time.Time `json:"last_turn_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// ListResult is a page of history, newest activity first.
type ListResult struct {
	Conversations []ConversationSummary `json:"conversations"`
	Limit         int                   `json:"limit"`
	HasMore       bool                  `json:"has_more"`
	NextCursor    *time.Time            `json:"next_cursor,omitempty"`
}

// Record stores one completed turn: the user's message and the reply it
// produced, plus the thread's new verdict and activity time. An empty
// conversationID opens a new thread in the same statement batch and returns its
// id; a non-empty one appends to a thread the user must own.
//
// One transaction, for a reason specific to this product. A turn here costs a
// live web search and then a generation on a single 6 GB card — tens of seconds
// of work that cannot be replayed, because researching the same subject tomorrow
// returns different evidence. Writing the pieces separately would let a failure
// between them leave a thread whose last turn is a question with no answer, and
// resuming that thread would send the model a history ending in a user turn it
// has already answered — it would research and answer it again, on the card that
// was the reason for bounding all of this in the first place. Opening the thread
// is inside the same transaction for the smaller version of that problem: a
// created-then-failed thread would sit in the sidebar with nothing in it.
//
// The ordinal is read inside the transaction rather than passed in, so a client
// that double-submits cannot invent positions; the unique index on
// (conversation_id, ordinal) turns the race into a failed insert.
func (s *Store) Record(
	ctx context.Context, userID, conversationID, userMessage string, reply Result,
) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit succeeded

	var next int
	if conversationID == "" {
		if err := tx.QueryRow(ctx,
			`INSERT INTO conversations (user_id, product, title)
			 VALUES ($1, $2, $3) RETURNING id`,
			userID, productPersona, DeriveTitle(userMessage),
		).Scan(&conversationID); err != nil {
			return "", err
		}
	} else {
		// Ownership is proven by the same statement that locks the row, so two
		// concurrent turns on one thread serialise here rather than racing for
		// the next ordinal.
		err = tx.QueryRow(ctx,
			`SELECT COALESCE(
			          (SELECT MAX(ordinal) + 1 FROM conversation_messages WHERE conversation_id = c.id),
			          0)
			   FROM conversations c
			  WHERE c.id = $1 AND c.user_id = $2
			  FOR UPDATE`,
			conversationID, userID).Scan(&next)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNoRows
		}
		if err != nil {
			return "", err
		}
	}

	sources, err := json.Marshal(nonNil(reply.Sources))
	if err != nil {
		return "", err
	}
	research, err := json.Marshal(nonNilSteps(reply.Research))
	if err != nil {
		return "", err
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO conversation_messages (conversation_id, ordinal, role, content)
		 VALUES ($1, $2, 'user', $3)`,
		conversationID, next, userMessage); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO conversation_messages
		   (conversation_id, ordinal, role, content, sources, research, model)
		 VALUES ($1, $2, 'assistant', $3, $4, $5, $6)`,
		conversationID, next+1, reply.Reply, sources, research, reply.Model); err != nil {
		return "", err
	}

	// The verdict is only overwritten when this turn produced one. A thread that
	// reached "Yatırılabilir" and is then asked a follow-up question keeps its
	// badge: the follow-up answer may be a clarifying question, and blanking the
	// decision because the newest message did not repeat it would lose a
	// conclusion the conversation genuinely reached.
	v := parseVerdict(reply.Reply)
	if v.Found {
		var score *int
		if v.Score >= 0 {
			score = &v.Score
		}
		_, err = tx.Exec(ctx,
			`UPDATE conversations
			    SET verdict = $2, verdict_score = $3, last_turn_at = now()
			  WHERE id = $1`,
			conversationID, v.Label, score)
	} else {
		_, err = tx.Exec(ctx,
			`UPDATE conversations SET last_turn_at = now() WHERE id = $1`, conversationID)
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return conversationID, nil
}

// List returns a page of the user's threads, most recently active first.
func (s *Store) List(ctx context.Context, userID string, limit int, before time.Time) (ListResult, error) {
	var beforeArg any
	if !before.IsZero() {
		beforeArg = before
	}

	rows, err := s.db.Query(ctx,
		`SELECT c.id, c.title, c.verdict, c.verdict_score,
		        (SELECT count(*) FROM conversation_messages m WHERE m.conversation_id = c.id),
		        c.last_turn_at, c.created_at
		   FROM conversations c
		  WHERE c.user_id = $1 AND c.product = $2
		    AND ($3::timestamptz IS NULL OR c.last_turn_at < $3)
		  ORDER BY c.last_turn_at DESC
		  LIMIT $4`,
		userID, productPersona, beforeArg, limit+1)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	out := ListResult{Conversations: []ConversationSummary{}, Limit: limit}
	for rows.Next() {
		var c ConversationSummary
		if err := rows.Scan(&c.ID, &c.Title, &c.Verdict, &c.VerdictScore,
			&c.Turns, &c.LastTurnAt, &c.CreatedAt); err != nil {
			return ListResult{}, err
		}
		out.Conversations = append(out.Conversations, c)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}

	if len(out.Conversations) > limit {
		out.Conversations = out.Conversations[:limit]
		out.HasMore = true
		cursor := out.Conversations[len(out.Conversations)-1].LastTurnAt
		out.NextCursor = &cursor
	}
	return out, nil
}

// Get returns one thread with its transcript, scoped to its owner.
func (s *Store) Get(ctx context.Context, userID, id string) (Conversation, error) {
	var c Conversation
	err := s.db.QueryRow(ctx,
		`SELECT id, title, verdict, verdict_score, last_turn_at, created_at
		   FROM conversations WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&c.ID, &c.Title, &c.Verdict, &c.VerdictScore, &c.LastTurnAt, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNoRows
	}
	if err != nil {
		return Conversation{}, err
	}

	rows, err := s.db.Query(ctx,
		`SELECT role, content, sources, research, model
		   FROM conversation_messages
		  WHERE conversation_id = $1
		  ORDER BY ordinal`, id)
	if err != nil {
		return Conversation{}, err
	}
	defer rows.Close()

	c.Messages = []Message{}
	for rows.Next() {
		var m Message
		var sources, research []byte
		if err := rows.Scan(&m.Role, &m.Content, &sources, &research, &m.Model); err != nil {
			return Conversation{}, err
		}
		// A message whose research will not decode still has its text, and the
		// text is the transcript. Dropping the whole thread because one JSONB
		// column went bad would destroy the record this table exists to keep, so
		// the citation trail degrades and the words survive.
		_ = json.Unmarshal(sources, &m.Sources)
		_ = json.Unmarshal(research, &m.Research)
		m.Sources = nonNil(m.Sources)
		m.Research = nonNilSteps(m.Research)
		c.Messages = append(c.Messages, m)
	}
	return c, rows.Err()
}

// Rename retitles a thread.
func (s *Store) Rename(ctx context.Context, userID, id, title string) error {
	title = clampTitle(title)
	if title == "" {
		return errors.New("empty title")
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE conversations SET title = $3 WHERE id = $1 AND user_id = $2`,
		id, userID, title)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// Delete removes a thread and, by cascade, its messages.
func (s *Store) Delete(ctx context.Context, userID, id string) error {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM conversations WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// SweepConversations removes every thread untouched for longer than olderThan.
//
// last_turn_at, not created_at: a thread someone is still using should not
// vanish from under them on its thirtieth day, and "untouched for a month" is
// what the retention period actually promises. The column is already indexed by
// idx_conversations_user_active.
//
// DELETE rather than the blanking the reports get. A report row carries
// measurements that aggregates depend on; a conversation carries none, so
// there is nothing to preserve and an emptied row would be litter.
// conversation_messages goes with it through ON DELETE CASCADE.
func (s *Store) SweepConversations(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM conversations WHERE last_turn_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeriveTitle names a thread after its opening message.
//
// Taken from what the user typed rather than generated, because a titling round
// trip would add a second generation to the slowest route in the system to
// produce something the opening line usually already is — people open these
// threads with a subject, not a sentence.
func DeriveTitle(first string) string {
	first = strings.TrimSpace(first)
	// First line only: an opener pasted from a deck can carry the whole summary
	// behind a newline, and only the first line is the subject.
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = strings.TrimSpace(first[:i])
	}
	if first == "" {
		return "Adsız değerlendirme"
	}
	return clampTitle(first)
}

// clampTitle bounds a title in runes, not bytes — Turkish titles are the norm
// here and a byte cut lands mid-character on ğ, ş or ı.
func clampTitle(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= titleMaxRunes {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:titleMaxRunes])) + "…"
}

// nonNil and nonNilSteps keep an absent slice out of the database as `[]` rather
// than `null`, so the column's shape matches its DEFAULT and readers never have
// to handle two spellings of empty.
func nonNil(s []Source) []Source {
	if s == nil {
		return []Source{}
	}
	return s
}

func nonNilSteps(s []ResearchStep) []ResearchStep {
	if s == nil {
		return []ResearchStep{}
	}
	return s
}
