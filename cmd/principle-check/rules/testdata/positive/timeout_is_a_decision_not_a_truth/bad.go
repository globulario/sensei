// Positive-control fixture for timeout_is_a_decision_not_a_truth.
// DeadlineExceeded branch immediately classifies a node as "down".
package badfix

import (
	"context"
	"errors"
)

type nodeT struct{ Status string }

func classify(err error, node *nodeT) {
	if errors.Is(err, context.DeadlineExceeded) {
		node.Status = "down" // BAD: timeout treated as proof of remote failure
	}
}

func evict(err error, removeMember func(string), id string) {
	if errors.Is(err, context.DeadlineExceeded) {
		removeMember(id) // BAD: destructive action on timeout
	}
}
