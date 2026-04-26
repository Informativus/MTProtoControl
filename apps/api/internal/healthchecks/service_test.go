package healthchecks

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mtproxy-control/apps/api/internal/database"
	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/migrations"
	"mtproxy-control/apps/api/internal/serverevents"
	"mtproxy-control/apps/api/internal/sshcredentials"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telegramalerts"
	"mtproxy-control/apps/api/internal/telemtconfig"
)

func TestServiceRunServerPersistsStateAndTransition(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	servers := inventory.NewRepository(db)
	publicHost := "mt.example.com"
	server, err := servers.CreateServer(ctx, inventory.CreateInput{
		Name:       "proxy_node_1",
		Host:       "203.0.113.10",
		SSHUser:    "operator",
		PublicHost: &publicHost,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	fields := telemtconfig.DefaultFields(server)
	configText, _, err := telemtconfig.Generate(fields)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	configs := telemtconfig.NewRepository(db)
	if _, err := configs.SaveRevision(ctx, server.ID, configText); err != nil {
		t.Fatalf("save config: %v", err)
	}

	credentials := sshcredentials.NewRepository(db)
	if err := credentials.RememberPrivateKeyPath(ctx, server.ID, "~/.ssh/proxy-node"); err != nil {
		t.Fatalf("remember private key path: %v", err)
	}

	health := NewRepository(db)
	ssh := &fakeExecutor{results: map[string]sshlayer.CommandResult{
		"health_container_status": {
			Name:       "health_container_status",
			Command:    "docker ps",
			Stdout:     "Up 2 minutes (healthy)\n",
			ExitCode:   0,
			DurationMS: 5,
			OK:         true,
		},
		"health_telemt_api": {
			Name:       "health_telemt_api",
			Command:    "curl",
			Stdout:     `{"users":[{"links":{"telegram":"https://t.me/proxy?server=mt.example.com&port=443&secret=test"}}]}`,
			ExitCode:   0,
			DurationMS: 5,
			OK:         true,
		},
	}}

	service := NewService(servers, configs, credentials, health, nil, nil, ssh)
	service.lookupHost = func(context.Context, string) ([]string, error) {
		return []string{"203.0.113.10"}, nil
	}
	service.dialContext = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
	service.now = func() time.Time {
		return time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	}

	first, err := service.RunServer(ctx, server)
	if err != nil {
		t.Fatalf("run first health check: %v", err)
	}
	if first.Check.Status != onlineStatus {
		t.Fatalf("expected first status %q, got %#v", onlineStatus, first.Check)
	}
	if !first.Check.DNSOK || !first.Check.TCPOK || !first.Check.SSHOK || !first.Check.DockerOK || !first.Check.TelemtAPIOK || !first.Check.LinkOK {
		t.Fatalf("expected all health checks to pass, got %#v", first.Check)
	}
	if first.Transition.Changed {
		t.Fatalf("expected first transition to be unchanged, got %#v", first.Transition)
	}

	storedServer, err := servers.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("get stored server: %v", err)
	}
	if storedServer.Status != onlineStatus || storedServer.LastCheckedAt == nil {
		t.Fatalf("expected stored server to be updated, got %#v", storedServer)
	}

	service.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("i/o timeout")
	}
	service.now = func() time.Time {
		return time.Date(2026, time.April, 25, 12, 1, 0, 0, time.UTC)
	}

	second, err := service.RunServer(ctx, server)
	if err != nil {
		t.Fatalf("run second health check: %v", err)
	}
	if second.Check.Status != offlineStatus {
		t.Fatalf("expected second status %q, got %#v", offlineStatus, second.Check)
	}
	if !second.Transition.Changed || second.Transition.PreviousStatus != onlineStatus || second.Transition.CurrentStatus != offlineStatus {
		t.Fatalf("expected online -> offline transition, got %#v", second.Transition)
	}

	history, err := health.ListByServer(ctx, server.ID, 10)
	if err != nil {
		t.Fatalf("list health history: %v", err)
	}
	if len(history) != 2 || history[0].Status != offlineStatus || history[1].Status != onlineStatus {
		t.Fatalf("expected latest-first history, got %#v", history)
	}
}

func TestServiceRunServerSendsTelegramAlertsOnStateChangeRepeatAndRecovery(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	servers := inventory.NewRepository(db)
	publicHost := "mt.example.com"
	server, err := servers.CreateServer(ctx, inventory.CreateInput{
		Name:       "proxy_node_1",
		Host:       "203.0.113.10",
		SSHUser:    "operator",
		PublicHost: &publicHost,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	fields := telemtconfig.DefaultFields(server)
	configText, _, err := telemtconfig.Generate(fields)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	configs := telemtconfig.NewRepository(db)
	if _, err := configs.SaveRevision(ctx, server.ID, configText); err != nil {
		t.Fatalf("save config: %v", err)
	}

	credentials := sshcredentials.NewRepository(db)
	if err := credentials.RememberPrivateKeyPath(ctx, server.ID, "~/.ssh/proxy-node"); err != nil {
		t.Fatalf("remember private key path: %v", err)
	}

	alertsRepo := telegramalerts.NewRepository(db)
	token := "123456:ABCDEF"
	chatID := "-100123456"
	enabled := true
	repeatMinutes := 30
	if _, err := alertsRepo.Save(ctx, telegramalerts.UpdateInput{
		TelegramBotToken:       &token,
		TelegramChatID:         &chatID,
		AlertsEnabled:          &enabled,
		RepeatDownAfterMinutes: &repeatMinutes,
	}); err != nil {
		t.Fatalf("save telegram settings: %v", err)
	}

	health := NewRepository(db)
	events := serverevents.NewRepository(db)
	sender := &fakeTelegramSender{}
	ssh := &fakeExecutor{results: map[string]sshlayer.CommandResult{
		"health_container_status": {
			Name:       "health_container_status",
			Command:    "docker ps",
			Stdout:     "Up 2 minutes (healthy)\n",
			ExitCode:   0,
			DurationMS: 5,
			OK:         true,
		},
		"health_telemt_api": {
			Name:       "health_telemt_api",
			Command:    "curl",
			Stdout:     `{"users":[{"links":{"telegram":"https://t.me/proxy?server=mt.example.com&port=443&secret=test"}}]}`,
			ExitCode:   0,
			DurationMS: 5,
			OK:         true,
		},
	}}

	service := NewService(servers, configs, credentials, health, events, telegramalerts.NewService(alertsRepo, sender), ssh)
	service.lookupHost = func(context.Context, string) ([]string, error) {
		return []string{"203.0.113.10"}, nil
	}
	service.dialContext = func(context.Context, string, string) (net.Conn, error) {
		client, peer := net.Pipe()
		_ = peer.Close()
		return client, nil
	}

	currentTime := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return currentTime }

	if _, err := service.RunServer(ctx, server); err != nil {
		t.Fatalf("run initial online health check: %v", err)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("expected no alert for initial healthy state, got %#v", sender.messages)
	}

	service.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("i/o timeout")
	}
	currentTime = time.Date(2026, time.April, 25, 12, 5, 0, 0, time.UTC)

	if _, err := service.RunServer(ctx, server); err != nil {
		t.Fatalf("run offline health check: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one offline alert, got %#v", sender.messages)
	}
	if !strings.Contains(sender.messages[0], "MTProto alert: proxy_node_1 offline") || !strings.Contains(sender.messages[0], "Last ok: 2026-04-25 12:00:00 UTC") {
		t.Fatalf("expected offline alert with last ok timestamp, got %q", sender.messages[0])
	}

	currentTime = time.Date(2026, time.April, 25, 12, 20, 0, 0, time.UTC)
	if _, err := service.RunServer(ctx, server); err != nil {
		t.Fatalf("run throttled offline health check: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected offline repeat to be throttled, got %#v", sender.messages)
	}

	currentTime = time.Date(2026, time.April, 25, 12, 40, 0, 0, time.UTC)
	if _, err := service.RunServer(ctx, server); err != nil {
		t.Fatalf("run repeated offline health check: %v", err)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("expected repeated offline alert after throttle window, got %#v", sender.messages)
	}
	if !strings.Contains(sender.messages[1], "Repeat: server is still offline") {
		t.Fatalf("expected repeated offline marker, got %q", sender.messages[1])
	}

	service.dialContext = func(context.Context, string, string) (net.Conn, error) {
		client, peer := net.Pipe()
		_ = peer.Close()
		return client, nil
	}
	currentTime = time.Date(2026, time.April, 25, 12, 43, 0, 0, time.UTC)

	if _, err := service.RunServer(ctx, server); err != nil {
		t.Fatalf("run recovery health check: %v", err)
	}
	if len(sender.messages) != 3 {
		t.Fatalf("expected recovery alert, got %#v", sender.messages)
	}
	if !strings.Contains(sender.messages[2], "MTProto alert: proxy_node_1 recovered") || !strings.Contains(sender.messages[2], "Downtime: 38m0s") {
		t.Fatalf("expected recovery alert with downtime, got %q", sender.messages[2])
	}

	alertEvents, err := events.ListByServer(ctx, server.ID, 10)
	if err != nil {
		t.Fatalf("list server events: %v", err)
	}
	if len(alertEvents) != 3 {
		t.Fatalf("expected three persisted alert events, got %#v", alertEvents)
	}
	if alertEvents[0].EventType != telegramalerts.EventTypeAlertRecovered || alertEvents[1].EventType != telegramalerts.EventTypeAlertOfflineRepeated || alertEvents[2].EventType != telegramalerts.EventTypeAlertOffline {
		t.Fatalf("unexpected alert event order: %#v", alertEvents)
	}
}

func openTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})

	if err := migrations.Up(context.Background(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return db
}

type fakeExecutor struct {
	results map[string]sshlayer.CommandResult
	errors  map[string]error
}

type fakeTelegramSender struct {
	messages []string
}

func (f *fakeTelegramSender) Send(_ context.Context, _, _, text string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeExecutor) Run(_ context.Context, _ sshlayer.TestRequest, request sshlayer.CommandRequest) (sshlayer.CommandResult, error) {
	if err := f.errors[request.Name]; err != nil {
		return sshlayer.CommandResult{}, err
	}
	result, ok := f.results[request.Name]
	if !ok {
		return sshlayer.CommandResult{}, nil
	}
	return result, nil
}

func (f *fakeExecutor) Upload(context.Context, sshlayer.TestRequest, sshlayer.UploadRequest) (sshlayer.CommandResult, error) {
	return sshlayer.CommandResult{}, nil
}
