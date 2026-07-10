// Positive-control fixture for timestamp_field_must_be_qualified.
// Struct with a bare `Timestamp int64` field and no semantic qualifier.
//
// NOTE: the field is the SOLE field of the struct on purpose. The rule's
// gogrep pattern `type $S struct { $*_; $name $T; $*_ }` only binds a
// field when it is the only field in the struct — an interior field
// (with siblings before/after) is NOT matched. That is a known matcher
// limitation shared with duration_vs_deadline_field_naming; both rules
// catch only single-field structs in production. Recorded as a coverage
// gap under meta.timestamp_is_an_observation_not_an_event_time.
package badfix

type event struct {
	Timestamp int64 // BAD: whose clock? what moment?
}
