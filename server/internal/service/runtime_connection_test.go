package service

import (
	"testing"
	"time"

	kernelpkg "poolx/internal/pkg/kernel"
)

func TestRuntimeConnectionRegistryRetainsClosedConnections(t *testing.T) {
	registry := &runtimeConnectionRegistry{active: make(map[string]RuntimeConnectionEntry)}
	started := time.Now().Add(-time.Minute)
	registry.active["connection-1"] = mapRuntimeConnection(kernelpkg.MihomoConnection{
		ID:          "connection-1",
		Start:       started,
		Upload:      10,
		Download:    20,
		Chains:      []string{"node-a", "GROUP-A"},
		Rule:        "DOMAIN",
		RulePayload: "example.com",
		Metadata: kernelpkg.MihomoConnectionMetadata{
			Network: "tcp", SourceIP: "127.0.0.1", SourcePort: "50000",
			Host: "example.com", DestinationPort: "443", InboundName: "entry-a",
		},
	}, "active", time.Now())

	registry.mu.Lock()
	previous := registry.active["connection-1"]
	endedAt := time.Now()
	previous.State = "closed"
	previous.EndedAt = &endedAt
	registry.history = append(registry.history, previous)
	delete(registry.active, "connection-1")
	registry.mu.Unlock()

	snapshot := registry.snapshot()
	if snapshot.ActiveCount != 0 || len(snapshot.Items) != 1 || snapshot.Items[0].State != "closed" {
		t.Fatalf("unexpected connection snapshot: %+v", snapshot)
	}
	if snapshot.Items[0].PolicyGroup != "GROUP-A" || snapshot.Items[0].OutboundNode != "node-a" {
		t.Fatalf("unexpected chain mapping: %+v", snapshot.Items[0])
	}
}
