// Positive-control fixture for ddl_must_be_idempotent.
// CREATE TABLE string literal WITHOUT IF NOT EXISTS, passed to a Query method.
package badfix

type session struct{}

func (s *session) Query(stmt string) *query { return &query{} }

type query struct{}

func (q *query) Exec() error { return nil }

func migrate(s *session) error {
	// BAD: CREATE TABLE without IF NOT EXISTS — fails on replay.
	return s.Query(`CREATE TABLE packages.artifacts (id text PRIMARY KEY, state text)`).Exec()
}
