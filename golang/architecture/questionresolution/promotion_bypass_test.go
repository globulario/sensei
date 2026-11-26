// SPDX-License-Identifier: AGPL-3.0-only

package questionresolution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// Architect ruling on #390: removing governed promotion's path coverage must
// not let an ordinary repository edit establish promoted knowledge. Here the
// exact record a promotion would write is put into the governed source by a
// plain repository write (the same owner sensei propose uses), with no
// promotion transaction. The record is in the corpus, but it is not promoted
// truth: certification still refuses the reusable answer.
func TestRepositoryEditOfTheRecordDoesNotSubstituteForPromotion(t *testing.T) {
	w := seedWorld(t)
	w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")

	if _, err := governedmutation.Apply(governedmutation.Request{RepositoryRoot: w.Repo, Proposal: proposedInvariant()}); err != nil {
		t.Fatalf("direct governed-source write: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(w.Repo, "docs", "awareness", "invariants.yaml"))
	if !strings.Contains(string(data), proposedInvariant().ID) {
		t.Fatal("the record was not written, so no bypass is attempted and this test asserts nothing")
	}

	res := w.certify(t)
	if res.Outcome != OutcomeIncompletePromotion {
		t.Fatalf("outcome = %s (%s): a repository edit stood in for a governed promotion", res.Outcome, res.Detail)
	}
	if res.Certificate != nil {
		t.Fatal("certified a reusable answer whose record was written without promotion")
	}
}
