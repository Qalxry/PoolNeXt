package service

import (
	"context"
	"fmt"
	"net"
	kernelpkg "poolx/internal/pkg/kernel"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	runtimeConnectionHistoryLimit = 300
	runtimeConnectionPollInterval = 2 * time.Second
)

type RuntimeConnectionEntry struct {
	ID              string     `json:"id"`
	State           string     `json:"state"`
	Network         string     `json:"network"`
	InboundType     string     `json:"inbound_type"`
	InboundName     string     `json:"inbound_name"`
	InboundUser     string     `json:"inbound_user,omitempty"`
	Source          string     `json:"source"`
	Destination     string     `json:"destination"`
	DestinationIP   string     `json:"destination_ip,omitempty"`
	Process         string     `json:"process,omitempty"`
	ProcessPath     string     `json:"process_path,omitempty"`
	Rule            string     `json:"rule,omitempty"`
	RulePayload     string     `json:"rule_payload,omitempty"`
	PolicyGroup     string     `json:"policy_group,omitempty"`
	OutboundNode    string     `json:"outbound_node,omitempty"`
	Chain           []string   `json:"chain"`
	ProviderChain   []string   `json:"provider_chain,omitempty"`
	Upload          int64      `json:"upload"`
	Download        int64      `json:"download"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationSeconds int64      `json:"duration_seconds"`
}

type RuntimeConnectionSnapshot struct {
	Available     bool                     `json:"available"`
	Error         string                   `json:"error,omitempty"`
	CollectedAt   time.Time                `json:"collected_at"`
	UploadRate    int64                    `json:"upload_rate"`
	DownloadRate  int64                    `json:"download_rate"`
	UploadTotal   int64                    `json:"upload_total"`
	DownloadTotal int64                    `json:"download_total"`
	Memory        uint64                   `json:"memory"`
	ActiveCount   int                      `json:"active_count"`
	Items         []RuntimeConnectionEntry `json:"items"`
}

type runtimeConnectionRegistry struct {
	mu            sync.Mutex
	cancel        context.CancelFunc
	generation    uint64
	active        map[string]RuntimeConnectionEntry
	history       []RuntimeConnectionEntry
	available     bool
	lastError     string
	collectedAt   time.Time
	uploadRate    int64
	downloadRate  int64
	uploadTotal   int64
	downloadTotal int64
	memory        uint64
}

var connectionRegistry = &runtimeConnectionRegistry{active: make(map[string]RuntimeConnectionEntry)}
var getMihomoConnections = kernelpkg.GetMihomoConnections
var getMihomoTraffic = kernelpkg.GetMihomoTraffic

func GetRuntimeConnections() *RuntimeConnectionSnapshot {
	return connectionRegistry.snapshot()
}

func (registry *runtimeConnectionRegistry) start(process *runtimeProcess) {
	if process == nil {
		return
	}
	registry.stop()
	ctx, cancel := context.WithCancel(context.Background())
	registry.mu.Lock()
	registry.generation++
	generation := registry.generation
	registry.cancel = cancel
	registry.active = make(map[string]RuntimeConnectionEntry)
	registry.available = false
	registry.lastError = "正在连接 Mihomo Controller"
	registry.collectedAt = time.Now()
	registry.mu.Unlock()
	go registry.run(ctx, generation, process.controllerAddress, process.controllerSecret)
}

func (registry *runtimeConnectionRegistry) stop() {
	registry.mu.Lock()
	now := time.Now()
	for _, previous := range registry.active {
		previous.State = "closed"
		previous.EndedAt = &now
		previous.DurationSeconds = maxDurationSeconds(previous.StartedAt, now)
		registry.history = append(registry.history, previous)
	}
	if len(registry.history) > runtimeConnectionHistoryLimit {
		registry.history = registry.history[len(registry.history)-runtimeConnectionHistoryLimit:]
	}
	registry.active = make(map[string]RuntimeConnectionEntry)
	registry.generation++
	if registry.cancel != nil {
		registry.cancel()
		registry.cancel = nil
	}
	registry.available = false
	registry.lastError = "Mihomo 未运行"
	registry.uploadRate = 0
	registry.downloadRate = 0
	registry.mu.Unlock()
}

func (registry *runtimeConnectionRegistry) run(ctx context.Context, generation uint64, address string, secret string) {
	registry.collect(ctx, generation, address, secret)
	ticker := time.NewTicker(runtimeConnectionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			registry.collect(ctx, generation, address, secret)
		}
	}
}

func (registry *runtimeConnectionRegistry) collect(parent context.Context, generation uint64, address string, secret string) {
	ctx, cancel := context.WithTimeout(parent, 1500*time.Millisecond)
	defer cancel()
	snapshot, err := getMihomoConnections(ctx, address, secret)
	if err != nil {
		registry.recordError(generation, err)
		return
	}

	trafficCtx, trafficCancel := context.WithTimeout(parent, 1500*time.Millisecond)
	traffic, trafficErr := getMihomoTraffic(trafficCtx, address, secret)
	trafficCancel()

	now := time.Now()
	next := make(map[string]RuntimeConnectionEntry, len(snapshot.Connections))
	for _, item := range snapshot.Connections {
		next[item.ID] = mapRuntimeConnection(item, "active", now)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.generation != generation {
		return
	}
	for id, previous := range registry.active {
		if _, ok := next[id]; ok {
			continue
		}
		endedAt := now
		previous.State = "closed"
		previous.EndedAt = &endedAt
		previous.DurationSeconds = maxDurationSeconds(previous.StartedAt, endedAt)
		registry.history = append(registry.history, previous)
	}
	if len(registry.history) > runtimeConnectionHistoryLimit {
		registry.history = registry.history[len(registry.history)-runtimeConnectionHistoryLimit:]
	}
	registry.active = next
	registry.available = true
	registry.lastError = ""
	registry.collectedAt = now
	registry.uploadTotal = snapshot.UploadTotal
	registry.downloadTotal = snapshot.DownloadTotal
	registry.memory = snapshot.Memory
	if trafficErr == nil && traffic != nil {
		registry.uploadRate = traffic.Up
		registry.downloadRate = traffic.Down
		registry.uploadTotal = traffic.UpTotal
		registry.downloadTotal = traffic.DownTotal
	} else {
		registry.uploadRate = 0
		registry.downloadRate = 0
	}
}

func (registry *runtimeConnectionRegistry) recordError(generation uint64, err error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.generation != generation {
		return
	}
	registry.available = false
	registry.lastError = err.Error()
	registry.collectedAt = time.Now()
	registry.uploadRate = 0
	registry.downloadRate = 0
}

func (registry *runtimeConnectionRegistry) snapshot() *RuntimeConnectionSnapshot {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	now := time.Now()
	items := make([]RuntimeConnectionEntry, 0, len(registry.active)+len(registry.history))
	for _, item := range registry.active {
		item.DurationSeconds = maxDurationSeconds(item.StartedAt, now)
		items = append(items, item)
	}
	sort.Slice(items, func(left int, right int) bool {
		return items[left].StartedAt.After(items[right].StartedAt)
	})
	for index := len(registry.history) - 1; index >= 0; index-- {
		items = append(items, registry.history[index])
	}
	return &RuntimeConnectionSnapshot{
		Available:     registry.available,
		Error:         registry.lastError,
		CollectedAt:   registry.collectedAt,
		UploadRate:    registry.uploadRate,
		DownloadRate:  registry.downloadRate,
		UploadTotal:   registry.uploadTotal,
		DownloadTotal: registry.downloadTotal,
		Memory:        registry.memory,
		ActiveCount:   len(registry.active),
		Items:         items,
	}
}

func mapRuntimeConnection(item kernelpkg.MihomoConnection, state string, now time.Time) RuntimeConnectionEntry {
	metadata := item.Metadata
	destinationHost := strings.TrimSpace(metadata.SniffHost)
	if destinationHost == "" {
		destinationHost = strings.TrimSpace(metadata.Host)
	}
	if destinationHost == "" {
		destinationHost = strings.TrimSpace(metadata.DestinationIP)
	}
	destination := joinAddress(destinationHost, metadata.DestinationPort)
	if destination == "" {
		destination = strings.TrimSpace(metadata.RemoteDestination)
	}
	source := joinAddress(metadata.SourceIP, metadata.SourcePort)
	chain := append([]string(nil), item.Chains...)
	policyGroup := ""
	outboundNode := ""
	if len(chain) > 0 {
		outboundNode = chain[0]
		policyGroup = chain[len(chain)-1]
	}
	return RuntimeConnectionEntry{
		ID:              item.ID,
		State:           state,
		Network:         strings.ToUpper(metadata.Network),
		InboundType:     metadata.Type,
		InboundName:     metadata.InboundName,
		InboundUser:     metadata.InboundUser,
		Source:          source,
		Destination:     destination,
		DestinationIP:   metadata.DestinationIP,
		Process:         metadata.Process,
		ProcessPath:     metadata.ProcessPath,
		Rule:            item.Rule,
		RulePayload:     item.RulePayload,
		PolicyGroup:     policyGroup,
		OutboundNode:    outboundNode,
		Chain:           chain,
		ProviderChain:   append([]string(nil), item.ProviderChains...),
		Upload:          item.Upload,
		Download:        item.Download,
		StartedAt:       item.Start,
		DurationSeconds: maxDurationSeconds(item.Start, now),
	}
}

func joinAddress(host string, port string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		return ""
	}
	if port == "" || port == "0" {
		return host
	}
	return net.JoinHostPort(host, port)
}

func maxDurationSeconds(start time.Time, end time.Time) int64 {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return int64(end.Sub(start).Seconds())
}

func resetRuntimeConnectionRegistryForTest() {
	connectionRegistry.stop()
	connectionRegistry.mu.Lock()
	defer connectionRegistry.mu.Unlock()
	connectionRegistry.active = make(map[string]RuntimeConnectionEntry)
	connectionRegistry.history = nil
	connectionRegistry.lastError = fmt.Sprintf("%s", "Mihomo 未运行")
}
