package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"poolx/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/websocket"
)

func TestStreamServerUpgradeLogsSurvivesServerWriteTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalHeartbeatInterval := serverUpgradeHeartbeatInterval
	serverUpgradeHeartbeatInterval = 80 * time.Millisecond
	t.Cleanup(func() {
		serverUpgradeHeartbeatInterval = originalHeartbeatInterval
	})

	engine := gin.New()
	engine.GET("/api/update/logs/ws", StreamServerUpgradeLogs)
	server := httptest.NewUnstartedServer(engine)
	server.Config.WriteTimeout = 20 * time.Millisecond
	server.Start()
	defer server.Close()

	webSocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/update/logs/ws"
	connection, err := websocket.Dial(webSocketURL, "", server.URL)
	if err != nil {
		t.Fatalf("dial upgrade log websocket: %v", err)
	}
	defer connection.Close()
	if err = connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set websocket deadline: %v", err)
	}

	var initial service.ServerUpgradeStreamSnapshot
	if err = websocket.JSON.Receive(connection, &initial); err != nil {
		t.Fatalf("receive initial upgrade snapshot: %v", err)
	}
	var heartbeat service.ServerUpgradeStreamSnapshot
	if err = websocket.JSON.Receive(connection, &heartbeat); err != nil {
		t.Fatalf("receive heartbeat after server WriteTimeout: %v", err)
	}
}

func TestClearResponseWriteDeadlineUsesZeroDeadline(t *testing.T) {
	writer := &deadlineAwareResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeDeadline:    time.Now().Add(time.Minute),
	}
	if err := clearResponseWriteDeadline(writer); err != nil {
		t.Fatalf("clear response write deadline: %v", err)
	}
	if !writer.writeDeadline.IsZero() {
		t.Fatalf("write deadline = %v, want zero", writer.writeDeadline)
	}
}

type deadlineAwareResponseWriter struct {
	*httptest.ResponseRecorder
	writeDeadline time.Time
}

func (writer *deadlineAwareResponseWriter) SetWriteDeadline(deadline time.Time) error {
	writer.writeDeadline = deadline
	return nil
}

var _ http.ResponseWriter = (*deadlineAwareResponseWriter)(nil)
