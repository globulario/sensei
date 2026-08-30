package transport_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// grpcDefaultRecvBytes is the limit this repair exists to lift. It is written
// here as a literal on purpose: the test must keep proving the boundary moved
// even if someone edits MaxMessageBytes.
const grpcDefaultRecvBytes = 4 << 20 // 4 MiB

type echoEditCheck struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	got int
}

func (e *echoEditCheck) EditCheck(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
	e.got = len(req.GetProposedContent())
	return &awarenesspb.EditCheckResponse{RulesEvaluated: 1}, nil
}

// dial stands up an AwarenessGraph server over bufconn. serverOpts and
// callOpts are applied verbatim so a test can show the SAME payload failing
// under gRPC's defaults and succeeding under the repaired ceiling.
func dial(t *testing.T, srv *echoEditCheck, serverOpts []grpc.ServerOption, callOpts []grpc.CallOption) awarenesspb.AwarenessGraphClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer(serverOpts...)
	awarenesspb.RegisterAwarenessGraphServer(s, srv)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	opts := []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if len(callOpts) > 0 {
		opts = append(opts, grpc.WithDefaultCallOptions(callOpts...))
	}
	conn, err := grpc.NewClient("passthrough://bufnet", opts...)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return awarenesspb.NewAwarenessGraphClient(conn)
}

// A file larger than gRPC's default receive limit must be EVALUATED, not
// refused by the transport.
//
// The gate sends a changed file's whole content in one unary EditCheck. Under
// the 4 MiB default, a 5.6 MB run log committed as a repository's own evidence
// came back as ResourceExhausted, which the enforcing gate correctly reported
// as CANNOT VERIFY. The refusal was right; the capability was wrong.
func TestEvidenceAboveTheOldCeilingIsEvaluated(t *testing.T) {
	if transport.MaxMessageBytes <= grpcDefaultRecvBytes {
		t.Fatalf("MaxMessageBytes = %d, which does not exceed gRPC's %d default: the boundary did not move",
			transport.MaxMessageBytes, grpcDefaultRecvBytes)
	}

	// The size that actually broke the gate, rounded up: 5.6 MB.
	payload := strings.Repeat("x", 5_646_967)
	if len(payload) <= grpcDefaultRecvBytes {
		t.Fatal("the specimen no longer exceeds the old ceiling, so it proves nothing")
	}

	srv := &echoEditCheck{}
	client := dial(t, srv,
		[]grpc.ServerOption{
			grpc.MaxRecvMsgSize(transport.MaxMessageBytes),
			grpc.MaxSendMsgSize(transport.MaxMessageBytes),
		},
		[]grpc.CallOption{
			grpc.MaxCallRecvMsgSize(transport.MaxMessageBytes),
			grpc.MaxCallSendMsgSize(transport.MaxMessageBytes),
		})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := client.EditCheck(ctx, &awarenesspb.EditCheckRequest{
		File:            "experiments/j1r-revision-identity/runs/J1R.complete-accepted.log",
		ProposedContent: payload,
		Domain:          "github.com/globulario/sensei-code",
	})
	if err != nil {
		t.Fatalf("EditCheck refused %d bytes: %v", len(payload), err)
	}
	if resp.GetRulesEvaluated() != 1 {
		t.Fatalf("RulesEvaluated = %d, want 1: the file was not evaluated", resp.GetRulesEvaluated())
	}
	if srv.got != len(payload) {
		t.Fatalf("server received %d bytes, want %d: the content did not arrive whole", srv.got, len(payload))
	}
}

// The same payload against gRPC's defaults still fails, which is what makes the
// test above evidence of a repair rather than a coincidence.
func TestTheOldCeilingReallyDidRefuseThisPayload(t *testing.T) {
	srv := &echoEditCheck{}
	client := dial(t, srv, nil, nil) // gRPC defaults on both ends

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := client.EditCheck(ctx, &awarenesspb.EditCheckRequest{
		ProposedContent: strings.Repeat("x", 5_646_967),
	})
	if err == nil {
		t.Fatal("the default ceiling accepted 5.6 MB, so this specimen never demonstrated the boundary")
	}
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted: %v", status.Code(err), err)
	}
}

// The ceiling is a stopgap, not a statement about how large evidence may be.
// If someone raises it far enough to look like a solution, this fails and asks
// for chunked transport with whole-object identity instead.
func TestTheCeilingIsStillAcknowledgedAsDebt(t *testing.T) {
	if transport.MaxMessageBytes > 64<<20 {
		t.Fatalf("MaxMessageBytes = %d: a fixed ceiling this large is being used in place of chunked transport",
			transport.MaxMessageBytes)
	}
}
