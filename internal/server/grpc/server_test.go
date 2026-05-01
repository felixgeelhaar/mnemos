package grpc_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/felixgeelhaar/bolt"
	mnemosgrpc "github.com/felixgeelhaar/mnemos/internal/server/grpc"
	"github.com/felixgeelhaar/mnemos/internal/store"
	_ "github.com/felixgeelhaar/mnemos/internal/store/memory"
	mnemosv1 "github.com/felixgeelhaar/mnemos/proto/gen/mnemos/v1"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testLogger() *bolt.Logger {
	return bolt.New(bolt.NewJSONHandler(os.Stderr))
}

func startTestServer(t *testing.T) (mnemosv1.MnemosServiceClient, func()) {
	t.Helper()
	conn, err := store.Open(context.Background(), "memory://")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	logger := testLogger()
	mnemosSrv := mnemosgrpc.NewServer(conn, nil, logger, "test")
	srv := grpclib.NewServer(grpclib.UnaryInterceptor(mnemosSrv.UnaryInterceptor()))
	mnemosSrv.Register(srv)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()

	cc, err := grpclib.NewClient(lis.Addr().String(), grpclib.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := mnemosv1.NewMnemosServiceClient(cc)

	cleanup := func() {
		_ = cc.Close()
		srv.GracefulStop()
		_ = conn.Close()
	}
	return client, cleanup
}

func TestHealth(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	resp, err := client.Health(context.Background(), &mnemosv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if resp.Version != "test" {
		t.Errorf("Version = %q, want test", resp.Version)
	}
}

func TestEventsRoundTrip(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := timestamppb.New(time.Now().UTC())

	// Append
	_, err := client.AppendEvents(ctx, &mnemosv1.AppendEventsRequest{
		Events: []*mnemosv1.Event{
			{Id: "ev-1", RunId: "r1", SchemaVersion: "v1", Content: "hello", SourceInputId: "in1", Timestamp: now, IngestedAt: now},
		},
	})
	if err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	// List
	list, err := client.ListEvents(ctx, &mnemosv1.ListEventsRequest{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(list.Events))
	}
	if list.Events[0].Id != "ev-1" {
		t.Errorf("event id = %q, want ev-1", list.Events[0].Id)
	}
}

func TestClaimsRoundTrip(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := timestamppb.New(time.Now().UTC())

	// Append claims
	_, err := client.AppendClaims(ctx, &mnemosv1.AppendClaimsRequest{
		Claims: []*mnemosv1.Claim{
			{Id: "cl-1", Text: "sky is blue", Type: "fact", Confidence: 0.9, Status: "active", CreatedAt: now},
		},
	})
	if err != nil {
		t.Fatalf("AppendClaims: %v", err)
	}

	// List
	list, err := client.ListClaims(ctx, &mnemosv1.ListClaimsRequest{})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(list.Claims) != 1 {
		t.Fatalf("len(claims) = %d, want 1", len(list.Claims))
	}
	if list.Claims[0].Text != "sky is blue" {
		t.Errorf("claim text = %q, want 'sky is blue'", list.Claims[0].Text)
	}
}

func TestRelationshipsRoundTrip(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := timestamppb.New(time.Now().UTC())

	// Need claims first since relationships reference them
	_, err := client.AppendClaims(ctx, &mnemosv1.AppendClaimsRequest{
		Claims: []*mnemosv1.Claim{
			{Id: "cl-a", Text: "a", Type: "fact", Confidence: 0.5, Status: "active", CreatedAt: now},
			{Id: "cl-b", Text: "b", Type: "fact", Confidence: 0.5, Status: "active", CreatedAt: now},
		},
	})
	if err != nil {
		t.Fatalf("AppendClaims: %v", err)
	}

	_, err = client.AppendRelationships(ctx, &mnemosv1.AppendRelationshipsRequest{
		Relationships: []*mnemosv1.Relationship{
			{Id: "rel-1", Type: "supports", FromClaimId: "cl-a", ToClaimId: "cl-b", CreatedAt: now},
		},
	})
	if err != nil {
		t.Fatalf("AppendRelationships: %v", err)
	}

	list, err := client.ListRelationships(ctx, &mnemosv1.ListRelationshipsRequest{})
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(list.Relationships) != 1 {
		t.Fatalf("len(relationships) = %d, want 1", len(list.Relationships))
	}
}

func TestEmbeddingsRoundTrip(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	_, err := client.AppendEmbeddings(ctx, &mnemosv1.AppendEmbeddingsRequest{
		Embeddings: []*mnemosv1.Embedding{
			{EntityId: "ev-1", EntityType: "event", Vector: []float32{0.1, 0.2, 0.3}, Model: "test", Dimensions: 3},
		},
	})
	if err != nil {
		t.Fatalf("AppendEmbeddings: %v", err)
	}

	list, err := client.ListEmbeddings(ctx, &mnemosv1.ListEmbeddingsRequest{})
	if err != nil {
		t.Fatalf("ListEmbeddings: %v", err)
	}
	if len(list.Embeddings) != 1 {
		t.Fatalf("len(embeddings) = %d, want 1", len(list.Embeddings))
	}
	if list.Embeddings[0].EntityId != "ev-1" {
		t.Errorf("entity id = %q, want ev-1", list.Embeddings[0].EntityId)
	}
}

func TestMetrics(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()
	now := timestamppb.New(time.Now().UTC())

	if _, err := client.AppendEvents(ctx, &mnemosv1.AppendEventsRequest{
		Events: []*mnemosv1.Event{{Id: "ev-1", RunId: "r1", SchemaVersion: "v1", Content: "x", SourceInputId: "in1", Timestamp: now, IngestedAt: now}},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	if _, err := client.AppendClaims(ctx, &mnemosv1.AppendClaimsRequest{
		Claims: []*mnemosv1.Claim{{Id: "cl-1", Text: "x", Type: "fact", Confidence: 0.5, Status: "active", CreatedAt: now}},
	}); err != nil {
		t.Fatalf("AppendClaims: %v", err)
	}

	m, err := client.Metrics(ctx, &mnemosv1.MetricsRequest{})
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.Events != 1 {
		t.Errorf("Events = %d, want 1", m.Events)
	}
	if m.Claims != 1 {
		t.Errorf("Claims = %d, want 1", m.Claims)
	}
}

func TestAppendEventsValidation(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	_, err := client.AppendEvents(context.Background(), &mnemosv1.AppendEventsRequest{})
	if err == nil {
		t.Fatal("expected error for empty events")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", st.Code())
	}
}

func TestAppendEventsEmptyID(t *testing.T) {
	client, cleanup := startTestServer(t)
	defer cleanup()

	_, err := client.AppendEvents(context.Background(), &mnemosv1.AppendEventsRequest{
		Events: []*mnemosv1.Event{{Id: ""}},
	})
	if err == nil {
		t.Fatal("expected error for empty event id")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", st.Code())
	}
}
