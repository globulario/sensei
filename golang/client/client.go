// SPDX-License-Identifier: AGPL-3.0-only

// Package client provides a standalone gRPC client for the awareness-graph
// service. No Globular-specific imports — any Go application can use this
// by importing "github.com/globulario/sensei/golang/client".
//
// Usage:
//
//	c, err := client.Dial("localhost:9090")             // insecure
//	c, err := client.Dial("host:9090", client.WithTLS(caCert, cert, key))
//	defer c.Close()
//
//	resp, err := c.Briefing(ctx, "golang/mcp/server.go", "", "standard", "")
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps a gRPC connection to an awareness-graph server.
type Client struct {
	cc   *grpc.ClientConn
	stub awarenesspb.AwarenessGraphClient
}

// Option configures how the client connects.
type Option func(*dialOpts) error

type dialOpts struct {
	creds credentials.TransportCredentials
	extra []grpc.DialOption
}

// WithTLS configures mTLS. Pass empty certFile/keyFile for server-only TLS.
func WithTLS(caFile, certFile, keyFile string) Option {
	return func(o *dialOpts) error {
		creds, err := loadTLS(caFile, certFile, keyFile)
		if err != nil {
			return err
		}
		o.creds = creds
		return nil
	}
}

// WithTransportCredentials sets arbitrary gRPC transport credentials.
func WithTransportCredentials(tc credentials.TransportCredentials) Option {
	return func(o *dialOpts) error {
		o.creds = tc
		return nil
	}
}

// WithDialOptions appends raw gRPC dial options.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *dialOpts) error {
		o.extra = append(o.extra, opts...)
		return nil
	}
}

// Dial connects to the awareness-graph server at addr (host:port).
// Without options, the connection is plaintext (suitable for localhost sidecar).
func Dial(addr string, opts ...Option) (*Client, error) {
	do := &dialOpts{}
	for _, o := range opts {
		if err := o(do); err != nil {
			return nil, fmt.Errorf("awareness-graph dial %s: %w", addr, err)
		}
	}

	var grpcOpts []grpc.DialOption
	if do.creds != nil {
		grpcOpts = append(grpcOpts, grpc.WithTransportCredentials(do.creds))
	} else {
		grpcOpts = append(grpcOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	// Attach the bearer token from $AWG_TOKEN when set, so every command that
	// dials through this helper authenticates to an auth-enabled server with no
	// per-command wiring. No-op when unset (trusted-network default).
	if cred := BearerToken(TokenFromEnv()); cred != nil {
		grpcOpts = append(grpcOpts, grpc.WithPerRPCCredentials(cred))
	}
	grpcOpts = append(grpcOpts, do.extra...)

	cc, err := grpc.NewClient(addr, grpcOpts...)
	if err != nil {
		return nil, fmt.Errorf("awareness-graph dial %s: %w", addr, err)
	}
	return &Client{
		cc:   cc,
		stub: awarenesspb.NewAwarenessGraphClient(cc),
	}, nil
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	if c.cc != nil {
		return c.cc.Close()
	}
	return nil
}

// Stub returns the raw generated gRPC client for advanced use cases.
func (c *Client) Stub() awarenesspb.AwarenessGraphClient {
	return c.stub
}

// ---------- RPC convenience methods ----------

// Briefing composes a prose briefing for a file or task.
//
// domain scopes the query to one repo/domain (e.g.
// "github.com/caddyserver/caddy"); it is required when the graph hosts more
// than one domain and must otherwise be "". Passing it here — rather than
// forcing callers down to Stub() — is what keeps this helper usable on a
// multi-domain graph, where an empty domain fails closed to an empty result.
func (c *Client) Briefing(ctx context.Context, file, task, depth, domain string) (*awarenesspb.BriefingResponse, error) {
	return c.stub.Briefing(ctx, &awarenesspb.BriefingRequest{
		File:   file,
		Task:   task,
		Depth:  depth,
		Domain: domain,
	})
}

// Impact returns the structured anchor surface for a file path. See Briefing
// for the meaning of domain.
func (c *Client) Impact(ctx context.Context, file, domain string) (*awarenesspb.ImpactResponse, error) {
	return c.stub.Impact(ctx, &awarenesspb.ImpactRequest{File: file, Domain: domain})
}

// Resolve fetches a single awareness node by class and bare id.
func (c *Client) Resolve(ctx context.Context, class, id, domain string) (*awarenesspb.ResolveResponse, error) {
	return c.stub.Resolve(ctx, &awarenesspb.ResolveRequest{Class: class, Id: id, Domain: domain})
}

// Query forwards a structured query to the awareness-graph service.
func (c *Client) Query(ctx context.Context, req *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error) {
	return c.stub.Query(ctx, req)
}

// Metadata returns graph-level coverage and freshness signals (graph-wide).
func (c *Client) Metadata(ctx context.Context) (*awarenesspb.MetadataResponse, error) {
	return c.stub.Metadata(ctx, &awarenesspb.MetadataRequest{})
}

// MetadataScoped is Metadata scoped to a domain/repo: per-class counts reflect
// only that domain (plus shared). Empty domain = graph-wide (same as Metadata).
func (c *Client) MetadataScoped(ctx context.Context, domain string) (*awarenesspb.MetadataResponse, error) {
	return c.stub.Metadata(ctx, &awarenesspb.MetadataRequest{Domain: domain})
}

// Preflight returns pre-edit decision support: risk class, required actions,
// forbidden fixes, and tests to run.
func (c *Client) Preflight(ctx context.Context, req *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	return c.stub.Preflight(ctx, req)
}

// EditCheck evaluates proposed file content against active repo-scoped advisory
// rules. It is warning-only: it never blocks and never edits code. Wiring it
// into the convenience surface (rather than leaving it Stub()-only) is what
// lets a client run a content-aware advisory pass at edit time. See Briefing
// for the meaning of domain.
func (c *Client) EditCheck(ctx context.Context, file, proposedContent, domain string) (*awarenesspb.EditCheckResponse, error) {
	return c.stub.EditCheck(ctx, &awarenesspb.EditCheckRequest{
		File:            file,
		ProposedContent: proposedContent,
		Domain:          domain,
	})
}

// Propose submits a typed feedback entry (failure_mode, invariant,
// required_test, forbidden_fix, or contract_unknown) to the awareness review
// queue — the agent write path. The entry is validated with contract-first
// rules; when valid it is written as a candidate (not a live graph node) for
// human/CI promotion. Requires the server started with -awareness-dir
// (`awg serve --enable-propose`); returns Unavailable otherwise.
func (c *Client) Propose(ctx context.Context, req *awarenesspb.ProposeRequest) (*awarenesspb.ProposeResponse, error) {
	return c.stub.Propose(ctx, req)
}

// ---------- TLS helper ----------

func loadTLS(caFile, certFile, keyFile string) (credentials.TransportCredentials, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if caFile != "" {
		ca, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("awareness-graph client: read CA %s: %w", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("awareness-graph client: invalid CA cert %s", caFile)
		}
		tlsCfg.RootCAs = pool
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("awareness-graph client: load keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return credentials.NewTLS(tlsCfg), nil
}
