package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mtproxy-control/apps/api/internal/database"
	"mtproxy-control/apps/api/internal/healthchecks"
	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/migrations"
	"mtproxy-control/apps/api/internal/serverrelationships"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telegramalerts"
)

func TestHealth(t *testing.T) {
	api := newTestAPI(t)

	response := api.request(t, http.MethodGet, "/health", "")

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}

	var payload map[string]string
	decodeResponse(t, response, &payload)

	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", payload["status"])
	}
}

func TestHealthRejectsUnsupportedMethods(t *testing.T) {
	api := newTestAPI(t)

	response := api.request(t, http.MethodPost, "/health", "")

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", response.Code)
	}
}

func TestHealthcheckSettingsEndpoint(t *testing.T) {
	api := newTestAPI(t, WithHealthCheckInterval(45*time.Second))

	response := api.request(t, http.MethodGet, "/api/healthchecks/settings", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload healthSettingsPayload
	decodeResponse(t, response, &payload)

	if payload.Interval != "45s" || payload.IntervalSeconds != 45 {
		t.Fatalf("expected configured 45s interval, got %#v", payload)
	}
}

func TestTelegramSettingsEndpointsMaskTokenAndSendTestAlert(t *testing.T) {
	api := newTestAPI(t)
	sender := &fakeTelegramAlertSender{}
	api.handler = NewWithOptions(api.db, WithTelegramAlertsService(telegramalerts.NewService(telegramalerts.NewRepository(api.db), sender))).Handler()

	response := api.request(t, http.MethodPut, "/api/settings/telegram", `{
		"telegram_bot_token":"123456:ABCDEF",
		"telegram_chat_id":"-100123456",
		"alerts_enabled":true,
		"repeat_down_after_minutes":45
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "123456:ABCDEF") {
		t.Fatalf("response leaked bot token: %s", response.Body.String())
	}

	var updated telegramSettingsPayload
	decodeResponse(t, response, &updated)
	if !updated.Settings.TelegramBotTokenConfigured || updated.Settings.TelegramBotTokenMasked != "1234...CDEF" {
		t.Fatalf("expected masked token metadata, got %#v", updated.Settings)
	}
	if updated.Settings.TelegramChatID != "-100123456" || !updated.Settings.AlertsEnabled || updated.Settings.RepeatDownAfterMinutes != 45 {
		t.Fatalf("unexpected saved telegram settings: %#v", updated.Settings)
	}

	getResponse := api.request(t, http.MethodGet, "/api/settings/telegram", "")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", getResponse.Code, getResponse.Body.String())
	}
	if strings.Contains(getResponse.Body.String(), "123456:ABCDEF") {
		t.Fatalf("GET response leaked bot token: %s", getResponse.Body.String())
	}

	var listed telegramSettingsPayload
	decodeResponse(t, getResponse, &listed)
	if !listed.Settings.TelegramBotTokenConfigured || listed.Settings.TelegramBotTokenMasked != "1234...CDEF" {
		t.Fatalf("expected masked token in GET response, got %#v", listed.Settings)
	}

	testResponse := api.request(t, http.MethodPost, "/api/settings/telegram/test", "")
	if testResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", testResponse.Code, testResponse.Body.String())
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "MTProto alert test") {
		t.Fatalf("expected one test telegram alert, got %#v", sender.messages)
	}
}

func TestServerHealthHistoryEndpoint(t *testing.T) {
	api := newTestAPI(t)
	created := mustCreateServer(t, api)
	repo := healthchecks.NewRepository(api.db)

	if _, _, err := repo.Append(context.Background(), healthchecks.CreateInput{
		ServerID:    created.Server.ID,
		Status:      "degraded",
		DNSOK:       true,
		TCPOK:       true,
		SSHOK:       false,
		TelemtAPIOK: false,
		DockerOK:    false,
		LinkOK:      true,
		Message:     "worker SSH checks skipped",
		CreatedAt:   time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("append degraded health check: %v", err)
	}
	if _, _, err := repo.Append(context.Background(), healthchecks.CreateInput{
		ServerID:    created.Server.ID,
		Status:      "online",
		DNSOK:       true,
		TCPOK:       true,
		SSHOK:       true,
		TelemtAPIOK: true,
		DockerOK:    true,
		LinkOK:      true,
		Message:     "All health checks passed.",
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append online health check: %v", err)
	}

	response := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/health?limit=2", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload healthHistoryPayload
	decodeResponse(t, response, &payload)

	if payload.Latest == nil || payload.Latest.Status != "online" {
		t.Fatalf("expected latest online health check, got %#v", payload.Latest)
	}
	if len(payload.Checks) != 2 || payload.Checks[1].Status != "degraded" {
		t.Fatalf("expected ordered health history, got %#v", payload.Checks)
	}
}

func TestServerCRUD(t *testing.T) {
	api := newTestAPI(t)

	createResponse := api.request(t, http.MethodPost, "/api/servers", `{
		"name":"proxy_node_1",
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"public_host":"mt.example.com"
	}`)

	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d with body %s", createResponse.Code, createResponse.Body.String())
	}

	var created serverPayload
	decodeResponse(t, createResponse, &created)

	if created.Server.ID == "" {
		t.Fatal("expected created server id")
	}
	if created.Server.SSHPort != 22 {
		t.Fatalf("expected default ssh_port 22, got %d", created.Server.SSHPort)
	}
	if created.Server.MTProtoPort != 443 {
		t.Fatalf("expected default mtproto_port 443, got %d", created.Server.MTProtoPort)
	}
	if created.Server.RemoteBasePath != inventory.DefaultRemoteBasePath {
		t.Fatalf("expected default remote path %q, got %q", inventory.DefaultRemoteBasePath, created.Server.RemoteBasePath)
	}

	listResponse := api.request(t, http.MethodGet, "/api/servers", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listResponse.Code)
	}

	var listed serversPayload
	decodeResponse(t, listResponse, &listed)

	if len(listed.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(listed.Servers))
	}
	if listed.Servers[0].ID != created.Server.ID {
		t.Fatalf("expected listed server id %q, got %q", created.Server.ID, listed.Servers[0].ID)
	}

	getResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID, "")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResponse.Code)
	}

	var fetched serverPayload
	decodeResponse(t, getResponse, &fetched)

	if fetched.Server.Name != "proxy_node_1" {
		t.Fatalf("expected server name proxy_node_1, got %q", fetched.Server.Name)
	}

	updatedAtBefore := fetched.Server.UpdatedAt
	patchResponse := api.request(t, http.MethodPatch, "/api/servers/"+created.Server.ID, `{
		"name":"proxy_node_primary",
		"mtproto_port":8443,
		"sni_domain":"mt.example.com"
	}`)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", patchResponse.Code, patchResponse.Body.String())
	}

	var updated serverPayload
	decodeResponse(t, patchResponse, &updated)

	if updated.Server.Name != "proxy_node_primary" {
		t.Fatalf("expected updated name, got %q", updated.Server.Name)
	}
	if updated.Server.MTProtoPort != 8443 {
		t.Fatalf("expected updated mtproto_port 8443, got %d", updated.Server.MTProtoPort)
	}
	if updated.Server.SNIDomain == nil || *updated.Server.SNIDomain != "mt.example.com" {
		t.Fatalf("expected updated sni_domain, got %#v", updated.Server.SNIDomain)
	}
	if !updated.Server.UpdatedAt.After(updatedAtBefore) {
		t.Fatalf("expected updated_at to move forward, before=%s after=%s", updatedAtBefore, updated.Server.UpdatedAt)
	}

	deleteResponse := api.request(t, http.MethodDelete, "/api/servers/"+created.Server.ID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", deleteResponse.Code)
	}

	notFoundResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID, "")
	if notFoundResponse.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 after delete, got %d", notFoundResponse.Code)
	}
}

func TestServerRelationshipEndpoints(t *testing.T) {
	api := newTestAPI(t)
	primary := mustCreateServerWithValues(t, api, "primary", "203.0.113.31")
	child := mustCreateServerWithValues(t, api, "child", "203.0.113.32")
	sharedIngressPeer := mustCreateServerWithValues(t, api, "shared-ingress-peer", "203.0.113.33")

	updateResponse := api.request(t, http.MethodPut, "/api/servers/"+primary.Server.ID+"/relationships", fmt.Sprintf(`{
		"relationships":[
			{"type":%q,"target_server_id":%q},
			{"type":%q,"target_server_id":%q}
		]
	}`,
		serverrelationships.TypeParentChild,
		child.Server.ID,
		serverrelationships.TypeSharedIngress,
		sharedIngressPeer.Server.ID,
	))
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", updateResponse.Code, updateResponse.Body.String())
	}

	var updated serverRelationshipsPayload
	decodeResponse(t, updateResponse, &updated)
	if len(updated.Relationships) != 2 {
		t.Fatalf("expected 2 attached relationships, got %#v", updated.Relationships)
	}
	assertRelationshipView(t, updated.Relationships, serverrelationships.TypeParentChild, "outgoing", child.Server.ID)
	assertRelationshipView(t, updated.Relationships, serverrelationships.TypeSharedIngress, "outgoing", sharedIngressPeer.Server.ID)

	childResponse := api.request(t, http.MethodGet, "/api/servers/"+child.Server.ID+"/relationships", "")
	if childResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", childResponse.Code, childResponse.Body.String())
	}

	var childRelationships serverRelationshipsPayload
	decodeResponse(t, childResponse, &childRelationships)
	if len(childRelationships.Relationships) != 1 {
		t.Fatalf("expected 1 child relationship, got %#v", childRelationships.Relationships)
	}
	assertRelationshipView(t, childRelationships.Relationships, serverrelationships.TypeParentChild, "incoming", primary.Server.ID)

	listResponse := api.request(t, http.MethodGet, "/api/server-relationships", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", listResponse.Code, listResponse.Body.String())
	}

	var listed allServerRelationshipsPayload
	decodeResponse(t, listResponse, &listed)
	if len(listed.Relationships) != 2 {
		t.Fatalf("expected 2 global relationships, got %#v", listed.Relationships)
	}
}

func TestServerRelationshipEndpointRejectsReverseSharedDuplicate(t *testing.T) {
	api := newTestAPI(t)
	left := mustCreateServerWithValues(t, api, "left", "203.0.113.41")
	right := mustCreateServerWithValues(t, api, "right", "203.0.113.42")

	seedResponse := api.request(t, http.MethodPut, "/api/servers/"+right.Server.ID+"/relationships", fmt.Sprintf(`{
		"relationships":[
			{"type":%q,"target_server_id":%q}
		]
	}`,
		serverrelationships.TypeSharedIngress,
		left.Server.ID,
	))
	if seedResponse.Code != http.StatusOK {
		t.Fatalf("expected seed status 200, got %d with body %s", seedResponse.Code, seedResponse.Body.String())
	}

	response := api.request(t, http.MethodPut, "/api/servers/"+left.Server.ID+"/relationships", fmt.Sprintf(`{
		"relationships":[
			{"type":%q,"target_server_id":%q}
		]
	}`,
		serverrelationships.TypeSharedIngress,
		right.Server.ID,
	))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d with body %s", response.Code, response.Body.String())
	}

	var payload errorPayload
	decodeResponse(t, response, &payload)
	if payload.Error.Details["relationships[0].target_server_id"] != "peer server already declares this shared relationship" {
		t.Fatalf("unexpected validation payload: %#v", payload.Error)
	}
}

func TestCreateServerValidationErrors(t *testing.T) {
	api := newTestAPI(t)

	response := api.request(t, http.MethodPost, "/api/servers", `{
		"name":"",
		"host":"",
		"ssh_user":"",
		"ssh_port":70000,
		"mtproto_port":0
	}`)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d with body %s", response.Code, response.Body.String())
	}

	var payload errorPayload
	decodeResponse(t, response, &payload)

	if payload.Error.Code != "validation_error" {
		t.Fatalf("expected validation_error, got %q", payload.Error.Code)
	}
	if payload.Error.Details["name"] != "is required" {
		t.Fatalf("expected name validation error, got %#v", payload.Error.Details)
	}
	if payload.Error.Details["ssh_port"] != "must be between 1 and 65535" {
		t.Fatalf("expected ssh_port validation error, got %#v", payload.Error.Details)
	}
}

func TestCreateServerRemembersPrivateKeyPath(t *testing.T) {
	api := newTestAPI(t)

	response := api.request(t, http.MethodPost, "/api/servers", `{
		"name":"proxy_node_1",
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"private_key_path":"~/.ssh/proxy-node"
	}`)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d with body %s", response.Code, response.Body.String())
	}

	var created serverPayload
	decodeResponse(t, response, &created)

	if created.Server.SavedPrivateKeyPath == nil || *created.Server.SavedPrivateKeyPath != "~/.ssh/proxy-node" {
		t.Fatalf("expected saved private key path in response, got %#v", created.Server.SavedPrivateKeyPath)
	}
}

func TestServerResponsesIncludeSavedPrivateKeyPath(t *testing.T) {
	api := newTestAPI(t)
	created := mustCreateServer(t, api)

	savedAt := time.Now().UTC().Add(-2 * time.Minute)
	if err := api.db.Exec(context.Background(), fmt.Sprintf(`
		.parameter init
		.parameter set @id %s
		.parameter set @server_id %s
		.parameter set @auth_type 'private_key_path'
		.parameter set @private_key_path '~/.ssh/proxy-node'
		.parameter set @created_at %s
		.parameter set @updated_at %s
		INSERT INTO ssh_credentials (
			id, server_id, auth_type, private_key_path, created_at, updated_at
		) VALUES (
			@id, @server_id, @auth_type, @private_key_path, @created_at, @updated_at
		);
	`, quoteValue("cred-saved-path"), quoteValue(created.Server.ID), quoteValue(savedAt.Format(time.RFC3339Nano)), quoteValue(savedAt.Format(time.RFC3339Nano)))); err != nil {
		t.Fatalf("insert ssh credential: %v", err)
	}

	getResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID, "")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResponse.Code)
	}

	var fetched serverPayload
	decodeResponse(t, getResponse, &fetched)

	if fetched.Server.SavedPrivateKeyPath == nil || *fetched.Server.SavedPrivateKeyPath != "~/.ssh/proxy-node" {
		t.Fatalf("expected saved private key path, got %#v", fetched.Server.SavedPrivateKeyPath)
	}
	if fetched.Server.SavedPrivateKeyPathUpdatedAt == nil || !fetched.Server.SavedPrivateKeyPathUpdatedAt.Equal(savedAt) {
		t.Fatalf("expected saved key updated_at %s, got %#v", savedAt, fetched.Server.SavedPrivateKeyPathUpdatedAt)
	}

	listResponse := api.request(t, http.MethodGet, "/api/servers", "")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listResponse.Code)
	}

	var listed serversPayload
	decodeResponse(t, listResponse, &listed)

	if len(listed.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(listed.Servers))
	}
	if listed.Servers[0].SavedPrivateKeyPath == nil || *listed.Servers[0].SavedPrivateKeyPath != "~/.ssh/proxy-node" {
		t.Fatalf("expected list payload to include saved key path, got %#v", listed.Servers[0].SavedPrivateKeyPath)
	}
}

func TestDeleteServerCascadesRelatedRecords(t *testing.T) {
	api := newTestAPI(t)

	createResponse := api.request(t, http.MethodPost, "/api/servers", `{
		"name":"proxy_node_1",
		"host":"203.0.113.10",
		"ssh_user":"operator"
	}`)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", createResponse.Code)
	}

	var created serverPayload
	decodeResponse(t, createResponse, &created)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := api.db.Exec(context.Background(), fmt.Sprintf(`
		.parameter init
		.parameter set @id %s
		.parameter set @server_id %s
		.parameter set @auth_type 'private_key_path'
		.parameter set @private_key_path '/tmp/test-key'
		.parameter set @created_at %s
		.parameter set @updated_at %s
		INSERT INTO ssh_credentials (
			id, server_id, auth_type, private_key_path, created_at, updated_at
		) VALUES (
			@id, @server_id, @auth_type, @private_key_path, @created_at, @updated_at
		);
	`, quoteValue("cred-1"), quoteValue(created.Server.ID), quoteValue(now), quoteValue(now))); err != nil {
		t.Fatalf("insert ssh credential: %v", err)
	}

	deleteResponse := api.request(t, http.MethodDelete, "/api/servers/"+created.Server.ID, "")
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", deleteResponse.Code)
	}

	var rows []struct {
		Count int `json:"count"`
	}
	if err := api.db.Query(context.Background(), fmt.Sprintf(`
		.parameter init
		.parameter set @server_id %s
		SELECT COUNT(1) AS count
		FROM ssh_credentials
		WHERE server_id = @server_id;
	`, quoteValue(created.Server.ID)), &rows); err != nil {
		t.Fatalf("count ssh credentials: %v", err)
	}
	count := 0
	if len(rows) > 0 {
		count = rows[0].Count
	}
	if count != 0 {
		t.Fatalf("expected cascaded delete, found %d related credential rows", count)
	}
}

func TestConfigGenerateAndCurrentEndpoints(t *testing.T) {
	api := newTestAPI(t)
	created := mustCreateServer(t, api)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/configs/generate", `{
		"public_host":"mt.example.com"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload configStatePayload
	decodeResponse(t, response, &payload)

	if payload.Current == nil {
		t.Fatal("expected current config")
	}
	if payload.Current.Version != 1 {
		t.Fatalf("expected version 1, got %d", payload.Current.Version)
	}
	if len(payload.Current.Fields.Secret) != 32 {
		t.Fatalf("expected 32-char secret, got %q", payload.Current.Fields.Secret)
	}
	if !strings.Contains(payload.Current.GeneratedLink, "secret=ee"+payload.Current.Fields.Secret) {
		t.Fatalf("expected ee link preview, got %q", payload.Current.GeneratedLink)
	}
	if len(payload.Revisions) != 1 {
		t.Fatalf("expected 1 revision, got %d", len(payload.Revisions))
	}
	if payload.DraftFields.TLSDomain != payload.DraftFields.PublicHost {
		t.Fatalf("expected tls_domain to default to public_host, got %#v", payload.DraftFields)
	}

	currentResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/configs/current", "")
	if currentResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", currentResponse.Code, currentResponse.Body.String())
	}

	var current configStatePayload
	decodeResponse(t, currentResponse, &current)
	if current.Current == nil || current.Current.Version != 1 {
		t.Fatalf("expected current config version 1, got %#v", current.Current)
	}
}

func TestUpdateCurrentConfigStoresRevision(t *testing.T) {
	api := newTestAPI(t)
	created := mustCreateServer(t, api)

	generated := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/configs/generate", `{
		"public_host":"mt.example.com"
	}`)
	if generated.Code != http.StatusOK {
		t.Fatalf("expected generate status 200, got %d with body %s", generated.Code, generated.Body.String())
	}

	var initial configStatePayload
	decodeResponse(t, generated, &initial)
	if initial.Current == nil {
		t.Fatal("expected current config after generate")
	}

	updatedText := strings.NewReplacer(
		`log_level = "normal"`, `log_level = "debug"`,
		"log_level = 'normal'", "log_level = 'debug'",
	).Replace(initial.Current.ConfigText)
	body, err := json.Marshal(updateCurrentConfigRequest{ConfigText: updatedText})
	if err != nil {
		t.Fatalf("marshal update request: %v", err)
	}

	updated := api.request(t, http.MethodPut, "/api/servers/"+created.Server.ID+"/configs/current", string(body))
	if updated.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d with body %s", updated.Code, updated.Body.String())
	}

	var payload configStatePayload
	decodeResponse(t, updated, &payload)
	if payload.Current == nil {
		t.Fatal("expected current config after update")
	}
	if payload.Current.Version != 2 {
		t.Fatalf("expected version 2, got %d", payload.Current.Version)
	}
	if payload.Current.Fields.LogLevel != "debug" {
		t.Fatalf("expected updated log level debug, got %q", payload.Current.Fields.LogLevel)
	}
	if len(payload.Revisions) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(payload.Revisions))
	}
}

func TestUpdateCurrentConfigCanMarkExistingRevisionApplied(t *testing.T) {
	api := newTestAPI(t)
	created := mustCreateServer(t, api)

	generated := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/configs/generate", `{
		"public_host":"mt.example.com"
	}`)
	if generated.Code != http.StatusOK {
		t.Fatalf("expected generate status 200, got %d with body %s", generated.Code, generated.Body.String())
	}

	body, err := json.Marshal(updateCurrentConfigRequest{MarkAsApplied: true})
	if err != nil {
		t.Fatalf("marshal apply import request: %v", err)
	}

	updated := api.request(t, http.MethodPut, "/api/servers/"+created.Server.ID+"/configs/current", string(body))
	if updated.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d with body %s", updated.Code, updated.Body.String())
	}

	var payload configStatePayload
	decodeResponse(t, updated, &payload)
	if payload.Current == nil {
		t.Fatal("expected current config after update")
	}
	if payload.Current.Version != 1 {
		t.Fatalf("expected version 1, got %d", payload.Current.Version)
	}
	if payload.Current.AppliedAt == nil {
		t.Fatal("expected applied_at to be stored")
	}
	if len(payload.Revisions) != 1 || payload.Revisions[0].AppliedAt == nil {
		t.Fatalf("expected applied revision metadata, got %#v", payload.Revisions)
	}
	if payload.Current.GeneratedLink == "" {
		t.Fatalf("expected generated link to stay populated, got %#v", payload.Current)
	}
}

func TestUpdateCurrentConfigRejectsInvalidTOML(t *testing.T) {
	api := newTestAPI(t)
	created := mustCreateServer(t, api)

	body, err := json.Marshal(updateCurrentConfigRequest{ConfigText: "[general"})
	if err != nil {
		t.Fatalf("marshal invalid request: %v", err)
	}

	response := api.request(t, http.MethodPut, "/api/servers/"+created.Server.ID+"/configs/current", string(body))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d with body %s", response.Code, response.Body.String())
	}

	var payload errorPayload
	decodeResponse(t, response, &payload)
	if payload.Error.Code != "validation_error" {
		t.Fatalf("expected validation_error, got %q", payload.Error.Code)
	}
	if payload.Error.Details["config_text"] == "" {
		t.Fatalf("expected config_text validation error, got %#v", payload.Error.Details)
	}
}

func TestDeployPreviewShowsPlanAndPortConflictDecision(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"check_docker":         okCommandResult("check_docker", "command -v docker", "/usr/bin/docker\n"),
			"check_docker_compose": okCommandResult("check_docker_compose", "docker compose version", "Docker Compose version v2.40.3\n"),
			"check_public_port": okCommandResult("check_public_port", "ss -ltnp",
				"LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:((\"nginx\",pid=10,fd=6))\n"),
			"check_remote_files":    okCommandResult("check_remote_files", "remote files", "config=present\ncompose=present\nbackups=present\n"),
			"check_panel_container": okCommandResult("check_panel_container", "docker ps", ""),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/deploy/preview", `{
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload deployPreviewPayload
	decodeResponse(t, response, &payload)

	if payload.Preview.RemoteBasePath != inventory.DefaultRemoteBasePath {
		t.Fatalf("expected remote base path %q, got %q", inventory.DefaultRemoteBasePath, payload.Preview.RemoteBasePath)
	}
	if payload.Preview.DockerImage != "ghcr.io/telemt/telemt:latest" {
		t.Fatalf("expected docker image, got %q", payload.Preview.DockerImage)
	}
	if len(payload.Preview.Files) != 3 {
		t.Fatalf("expected 3 planned files, got %d", len(payload.Preview.Files))
	}
	if !payload.Preview.Files[0].WillBackup || !payload.Preview.Files[1].WillBackup {
		t.Fatalf("expected config and compose backups, got %#v", payload.Preview.Files)
	}
	if payload.Preview.RequiredDecision == nil {
		t.Fatal("expected required decision for occupied public port")
	}
	if payload.Preview.RequiredDecision.Options[0] != "stop_existing_service" {
		t.Fatalf("expected stop_existing_service option first, got %#v", payload.Preview.RequiredDecision.Options)
	}
	if len(payload.Preview.Risks) == 0 || payload.Preview.Risks[0].Code != "public_port_in_use" {
		t.Fatalf("expected public port risk, got %#v", payload.Preview.Risks)
	}
	if executor.lastRequest.AuthType != sshlayer.AuthTypePrivateKeyPath {
		t.Fatalf("expected deploy auth type private_key_path, got %q", executor.lastRequest.AuthType)
	}
	if executor.lastRequest.PrivateKeyPath == nil || *executor.lastRequest.PrivateKeyPath == "" {
		t.Fatalf("expected forwarded private_key_path, got %#v", executor.lastRequest.PrivateKeyPath)
	}
	runRequest, ok := executor.runRequests["check_panel_container"]
	if !ok {
		t.Fatalf("expected check_panel_container request, got %#v", executor.runRequests)
	}
	if !strings.Contains(runRequest.Command, `--format "{{.Names}}\t{{.Status}}"`) {
		t.Fatalf("expected shell-safe docker format command, got %q", runRequest.Command)
	}
}

func TestDeployPreviewForwardsPasswordAuth(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"check_docker":          okCommandResult("check_docker", "command -v docker", "/usr/bin/docker\n"),
			"check_docker_compose":  okCommandResult("check_docker_compose", "docker compose version", "Docker Compose version v2.40.3\n"),
			"check_public_port":     okCommandResult("check_public_port", "ss -ltnp", ""),
			"check_remote_files":    okCommandResult("check_remote_files", "remote files", "config=missing\ncompose=missing\nbackups=missing\n"),
			"check_panel_container": okCommandResult("check_panel_container", "docker ps", ""),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/deploy/preview", `{
		"auth_type":"password",
		"password":"hunter2"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if executor.lastRequest.AuthType != sshlayer.AuthTypePassword {
		t.Fatalf("expected password auth type, got %q", executor.lastRequest.AuthType)
	}
	if executor.lastRequest.Password == nil || *executor.lastRequest.Password != "hunter2" {
		t.Fatalf("expected forwarded password, got %#v", executor.lastRequest.Password)
	}
}

func TestDeployApplyBlockedWithoutConfirmation(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"check_docker":         okCommandResult("check_docker", "command -v docker", "/usr/bin/docker\n"),
			"check_docker_compose": okCommandResult("check_docker_compose", "docker compose version", "Docker Compose version v2.40.3\n"),
			"check_public_port": okCommandResult("check_public_port", "ss -ltnp",
				"LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:((\"nginx\",pid=10,fd=6))\n"),
			"check_remote_files":    okCommandResult("check_remote_files", "remote files", "config=missing\ncompose=missing\nbackups=missing\n"),
			"check_panel_container": okCommandResult("check_panel_container", "docker ps", ""),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/deploy/apply", `{
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d with body %s", response.Code, response.Body.String())
	}

	var payload deployErrorPayload
	decodeResponse(t, response, &payload)

	if payload.Error.Code != "deploy_blocked" {
		t.Fatalf("expected deploy_blocked, got %q", payload.Error.Code)
	}
	if payload.Preview == nil || payload.Preview.RequiredDecision == nil {
		t.Fatalf("expected preview with required decision, got %#v", payload.Preview)
	}
	for _, call := range executor.calls {
		if call == "run:create_remote_directories" {
			t.Fatalf("expected apply steps not to run, calls=%#v", executor.calls)
		}
	}
}

func TestDeployApplySuccessStoresGeneratedLinkAndEvents(t *testing.T) {
	generatedLink := "https://t.me/proxy?server=mt.example.com&port=443&secret=ee0123456789abcdef"
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"check_docker":              okCommandResult("check_docker", "command -v docker", "/usr/bin/docker\n"),
			"check_docker_compose":      okCommandResult("check_docker_compose", "docker compose version", "Docker Compose version v2.40.3\n"),
			"check_public_port":         okCommandResult("check_public_port", "ss -ltnp", "LISTEN 0 4096 127.0.0.1:9091 0.0.0.0:* users:((\"telemt\",pid=42,fd=6))\n"),
			"check_remote_files":        okCommandResult("check_remote_files", "remote files", "config=missing\ncompose=missing\nbackups=present\n"),
			"check_panel_container":     okCommandResult("check_panel_container", "docker ps", ""),
			"create_remote_directories": okCommandResult("create_remote_directories", "mkdir -p", ""),
			"backup_existing_files":     okCommandResult("backup_existing_files", "cp", ""),
			"docker_compose_up":         okCommandResult("docker_compose_up", "docker compose up -d", "Container started\n"),
			"wait_container_health":     okCommandResult("wait_container_health", "wait health", "status=healthy\n"),
			"query_telemt_api":          okCommandResult("query_telemt_api", "curl", `{"users":[{"links":{"telegram":"`+generatedLink+`"}}]}`),
		},
		uploadResults: map[string]sshlayer.CommandResult{
			"upload_config":  okCommandResult("upload_config", "upload config", ""),
			"upload_compose": okCommandResult("upload_compose", "upload compose", ""),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/deploy/apply", `{
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload deployApplyPayload
	decodeResponse(t, response, &payload)

	if payload.Result.GeneratedLink != generatedLink {
		t.Fatalf("expected generated link %q, got %q", generatedLink, payload.Result.GeneratedLink)
	}
	if len(payload.Result.Events) != 7 {
		t.Fatalf("expected 7 deploy events, got %d", len(payload.Result.Events))
	}
	if payload.Config.Current == nil {
		t.Fatal("expected updated current config in response")
	}
	if payload.Config.Current.GeneratedLink != generatedLink {
		t.Fatalf("expected stored generated link %q, got %q", generatedLink, payload.Config.Current.GeneratedLink)
	}
	if payload.Config.Current.AppliedAt == nil {
		t.Fatal("expected applied_at to be stored")
	}

	var rows []struct {
		Count int `json:"count"`
	}
	if err := api.db.Query(context.Background(), fmt.Sprintf(`
		SELECT COUNT(1) AS count
		FROM server_events
		WHERE server_id = %s;
	`, quoteValue(created.Server.ID)), &rows); err != nil {
		t.Fatalf("count server events: %v", err)
	}
	if len(rows) == 0 || rows[0].Count != 7 {
		t.Fatalf("expected 7 persisted server events, got %#v", rows)
	}
}

func TestDeployApplyFailureSendsTelegramAlert(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"check_docker":              okCommandResult("check_docker", "command -v docker", "/usr/bin/docker\n"),
			"check_docker_compose":      okCommandResult("check_docker_compose", "docker compose version", "Docker Compose version v2.40.3\n"),
			"check_public_port":         okCommandResult("check_public_port", "ss -ltnp", "LISTEN 0 4096 127.0.0.1:9091 0.0.0.0:* users:((\"telemt\",pid=42,fd=6))\n"),
			"check_remote_files":        okCommandResult("check_remote_files", "remote files", "config=missing\ncompose=missing\nbackups=present\n"),
			"check_panel_container":     okCommandResult("check_panel_container", "docker ps", ""),
			"create_remote_directories": okCommandResult("create_remote_directories", "mkdir -p", ""),
			"backup_existing_files":     okCommandResult("backup_existing_files", "cp", ""),
			"docker_compose_up":         okCommandResult("docker_compose_up", "docker compose up -d", "Container started\n"),
			"wait_container_health":     okCommandResult("wait_container_health", "wait health", "status=healthy\n"),
			"query_telemt_api":          okCommandResult("query_telemt_api", "curl", `{}`),
		},
		uploadResults: map[string]sshlayer.CommandResult{
			"upload_config":  okCommandResult("upload_config", "upload config", ""),
			"upload_compose": okCommandResult("upload_compose", "upload compose", ""),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	sender := &fakeTelegramAlertSender{}
	api.handler = NewWithOptions(api.db, WithSSHExecutor(executor), WithTelegramAlertsService(telegramalerts.NewService(telegramalerts.NewRepository(api.db), sender))).Handler()

	settingsResponse := api.request(t, http.MethodPut, "/api/settings/telegram", `{
		"telegram_bot_token":"123456:ABCDEF",
		"telegram_chat_id":"-100123456",
		"alerts_enabled":true,
		"repeat_down_after_minutes":0
	}`)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("expected telegram settings status 200, got %d with body %s", settingsResponse.Code, settingsResponse.Body.String())
	}

	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/deploy/apply", `{
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d with body %s", response.Code, response.Body.String())
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "deploy failed") {
		t.Fatalf("expected one deploy failure telegram alert, got %#v", sender.messages)
	}
}

func TestRestartEndpointRunsComposeRestartAndPersistsEvent(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"restart_compose": okCommandResult("restart_compose", "docker compose restart", "telemt restarted\n"),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/restart", `{
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload restartPayload
	decodeResponse(t, response, &payload)

	if !payload.Result.Result.OK {
		t.Fatalf("expected restart result ok, got %#v", payload.Result.Result)
	}
	if len(payload.Result.Events) != 1 {
		t.Fatalf("expected 1 restart event, got %d", len(payload.Result.Events))
	}
	runRequest, ok := executor.runRequests["restart_compose"]
	if !ok {
		t.Fatalf("expected restart_compose request, got %#v", executor.runRequests)
	}
	if !strings.Contains(runRequest.Command, "docker compose restart") || !strings.Contains(runRequest.Command, inventory.DefaultRemoteBasePath) {
		t.Fatalf("expected restart command to use remote base path, got %q", runRequest.Command)
	}
	if !strings.Contains(runRequest.Command, `docker ps -a --format "{{.Names}} {{.Ports}}"`) {
		t.Fatalf("expected restart command to try resolving container by ports, got %q", runRequest.Command)
	}
	if !strings.Contains(runRequest.Command, `docker ps -a --format "{{.Names}} {{.Image}}"`) {
		t.Fatalf("expected restart command to try resolving container by image, got %q", runRequest.Command)
	}
	if !strings.Contains(runRequest.Command, `docker restart "$container"`) {
		t.Fatalf("expected restart fallback to use resolved container variable, got %q", runRequest.Command)
	}

	var rows []struct {
		Count int `json:"count"`
	}
	if err := api.db.Query(context.Background(), fmt.Sprintf(`
		SELECT COUNT(1) AS count
		FROM server_events
		WHERE server_id = %s AND event_type = 'restart_compose';
	`, quoteValue(created.Server.ID)), &rows); err != nil {
		t.Fatalf("count restart events: %v", err)
	}
	if len(rows) == 0 || rows[0].Count != 1 {
		t.Fatalf("expected 1 persisted restart event, got %#v", rows)
	}
}

func TestLogsEndpointReturnsComposeLogs(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"logs_telemt": okCommandResult("logs_telemt", "docker compose logs", "line one\nline two\n"),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/logs?private_key_path=~/.ssh/proxy-node&tail=50", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload logsPayload
	decodeResponse(t, response, &payload)

	if payload.Logs.Result.Stdout != "line one\nline two\n" {
		t.Fatalf("expected logs stdout, got %#v", payload.Logs.Result)
	}
	runRequest, ok := executor.runRequests["logs_telemt"]
	if !ok {
		t.Fatalf("expected logs_telemt request, got %#v", executor.runRequests)
	}
	if !strings.Contains(runRequest.Command, "docker compose logs --tail=50 telemt") {
		t.Fatalf("expected logs tail command, got %q", runRequest.Command)
	}
	if !strings.Contains(runRequest.Command, `docker ps -a --format "{{.Names}} {{.Ports}}"`) {
		t.Fatalf("expected logs command to try resolving container by ports, got %q", runRequest.Command)
	}
	if !strings.Contains(runRequest.Command, `docker ps -a --format "{{.Names}} {{.Image}}"`) {
		t.Fatalf("expected logs command to try resolving container by image, got %q", runRequest.Command)
	}
	if !strings.Contains(runRequest.Command, `docker logs --tail=50 "$container"`) {
		t.Fatalf("expected docker logs fallback to use resolved container variable, got %q", runRequest.Command)
	}
}

func TestLogsStreamEndpointWritesInitialSnapshot(t *testing.T) {
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"logs_telemt": okCommandResult("logs_telemt", "docker compose logs", "stream line\n"),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor), WithLogStreamPollInterval(10*time.Millisecond))
	created := mustCreateServer(t, api)

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/servers/"+created.Server.ID+"/logs/stream?private_key_path=~/.ssh/proxy-node", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		api.handler.ServeHTTP(recorder, request)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not exit after request cancellation")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "event: logs") {
		t.Fatalf("expected SSE logs event, got %q", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "stream line") {
		t.Fatalf("expected streamed log line, got %q", recorder.Body.String())
	}
}

func TestStatusEndpointReturnsConfigHealthAndLiveChecks(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for tcp check: %v", err)
	}
	defer listener.Close()

	publicPort := listener.Addr().(*net.TCPAddr).Port
	liveLink := "https://t.me/proxy?server=127.0.0.1&port=443&secret=ee0123456789abcdef"
	executor := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"status_container":  okCommandResult("status_container", "docker ps", "Up 4 minutes (healthy)\n"),
			"status_telemt_api": okCommandResult("status_telemt_api", "curl", `{"users":[{"links":{"telegram":"`+liveLink+`"}}]}`),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))

	createResponse := api.request(t, http.MethodPost, "/api/servers", fmt.Sprintf(`{
		"name":"loopback",
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"public_host":"127.0.0.1",
		"mtproto_port":%d
	}`, publicPort))
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d with body %s", createResponse.Code, createResponse.Body.String())
	}

	var created serverPayload
	decodeResponse(t, createResponse, &created)

	generateResponse := api.request(t, http.MethodPost, "/api/servers/"+created.Server.ID+"/configs/generate", fmt.Sprintf(`{
		"public_host":"127.0.0.1",
		"public_port":%d
	}`, publicPort))
	if generateResponse.Code != http.StatusOK {
		t.Fatalf("expected config generate status 200, got %d with body %s", generateResponse.Code, generateResponse.Body.String())
	}

	if err := api.db.Exec(context.Background(), fmt.Sprintf(`
		.parameter init
		.parameter set @id %s
		.parameter set @server_id %s
		.parameter set @status 'healthy'
		.parameter set @message 'latest worker check'
		.parameter set @created_at %s
		INSERT INTO health_checks (
			id, server_id, status, tcp_ok, telemt_api_ok, docker_ok, latency_ms, message, created_at
		) VALUES (
			@id, @server_id, @status, 1, 1, 1, 24, @message, @created_at
		);
	`, quoteValue("health-1"), quoteValue(created.Server.ID), quoteValue(time.Now().UTC().Format(time.RFC3339Nano)))); err != nil {
		t.Fatalf("insert health check: %v", err)
	}

	response := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/status?private_key_path=~/.ssh/proxy-node", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload statusPayload
	decodeResponse(t, response, &payload)

	if payload.Status.Container.Status != "ok" {
		t.Fatalf("expected container ok, got %#v", payload.Status.Container)
	}
	if payload.Status.TelemtAPI.Status != "ok" || payload.Status.TelemtAPI.UserCount != 1 {
		t.Fatalf("expected telemt api ok with one user, got %#v", payload.Status.TelemtAPI)
	}
	if !payload.Status.PublicPort.Checked || !payload.Status.PublicPort.Reachable {
		t.Fatalf("expected reachable public port, got %#v", payload.Status.PublicPort)
	}
	if payload.Status.LatestHealth == nil || payload.Status.LatestHealth.Status != "healthy" {
		t.Fatalf("expected latest health check, got %#v", payload.Status.LatestHealth)
	}
	if payload.Status.CurrentConfig == nil || payload.Status.CurrentConfig.Version != 1 {
		t.Fatalf("expected current config version 1, got %#v", payload.Status.CurrentConfig)
	}
	if payload.Status.GeneratedLink != liveLink || payload.Status.GeneratedLinkSource != "telemt_api" {
		t.Fatalf("expected live link from Telemt API, got %#v", payload.Status)
	}
	runRequest, ok := executor.runRequests["status_container"]
	if !ok {
		t.Fatalf("expected status_container request, got %#v", executor.runRequests)
	}
	if !strings.Contains(runRequest.Command, `docker ps -a --format "{{.Names}} {{.Status}}"`) {
		t.Fatalf("expected shell-safe status_container command, got %q", runRequest.Command)
	}
}

func TestLinkEndpointFallsBackToStoredConfigLink(t *testing.T) {
	executor := &fakeSSHExecutor{
		runErrors: map[string]error{
			"link_telemt_api": &sshlayer.OperationError{Kind: sshlayer.ErrorKindConnect, Message: "ssh connection timed out"},
		},
	}
	api := newTestAPI(t, WithSSHExecutor(executor))
	created := mustCreateServer(t, api)
	config := mustGenerateConfig(t, api, created.Server.ID)

	response := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/link?private_key_path=~/.ssh/proxy-node", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload linkPayload
	decodeResponse(t, response, &payload)

	if payload.Link.GeneratedLink != config.Current.GeneratedLink {
		t.Fatalf("expected stored config link fallback, got %#v", payload.Link)
	}
	if payload.Link.Source != "config_revision" {
		t.Fatalf("expected config_revision source, got %#v", payload.Link)
	}
	if payload.Link.Warning == "" {
		t.Fatalf("expected fallback warning, got %#v", payload.Link)
	}
}

func TestSSHTestEndpoint(t *testing.T) {
	fake := &fakeSSHTester{
		result: sshlayer.TestResult{
			OK: true,
			Facts: sshlayer.ServerFacts{
				Hostname:             "proxy-node-1",
				CurrentUser:          "operator",
				Architecture:         "x86_64",
				DockerVersion:        "Docker version 29.4.1",
				DockerComposeVersion: "Docker Compose version v2.40.3",
			},
			Commands: []sshlayer.CommandResult{{
				Name:       "hostname",
				Command:    "hostname",
				Stdout:     "proxy-node-1\n",
				Stderr:     "",
				ExitCode:   0,
				DurationMS: 15,
				TimedOut:   false,
				OK:         true,
			}},
		},
	}
	api := newTestAPI(t, WithSSHTester(fake))

	response := api.request(t, http.MethodPost, "/api/ssh/test", `{
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"private_key_text",
		"private_key_text":"-----BEGIN OPENSSH PRIVATE KEY-----\\nsecret\\n-----END OPENSSH PRIVATE KEY-----",
		"passphrase":"hunter2"
	}`)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "hunter2") {
		t.Fatalf("response leaked passphrase: %s", response.Body.String())
	}

	var payload sshlayer.TestResult
	decodeResponse(t, response, &payload)

	if !payload.OK {
		t.Fatal("expected ok=true")
	}
	if payload.Facts.Hostname != "proxy-node-1" {
		t.Fatalf("expected hostname proxy-node-1, got %q", payload.Facts.Hostname)
	}
	if fake.request.Host != "203.0.113.10" {
		t.Fatalf("expected forwarded host, got %q", fake.request.Host)
	}
	if fake.request.AuthType != sshlayer.AuthTypePrivateKeyText {
		t.Fatalf("expected forwarded auth type, got %q", fake.request.AuthType)
	}
}

func TestSSHTestEndpointSupportsPasswordAuthWithoutLeakingPassword(t *testing.T) {
	fake := &fakeSSHTester{
		result: sshlayer.TestResult{OK: true},
	}
	api := newTestAPI(t, WithSSHTester(fake))

	response := api.request(t, http.MethodPost, "/api/ssh/test", `{
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"password",
		"password":"hunter2"
	}`)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "hunter2") {
		t.Fatalf("response leaked password: %s", response.Body.String())
	}
	if fake.request.AuthType != sshlayer.AuthTypePassword {
		t.Fatalf("expected password auth type, got %q", fake.request.AuthType)
	}
	if fake.request.Password == nil || *fake.request.Password != "hunter2" {
		t.Fatalf("expected forwarded password, got %#v", fake.request.Password)
	}
}

func TestSSHTestEndpointRemembersPrivateKeyPathForServer(t *testing.T) {
	fake := &fakeSSHTester{
		result: sshlayer.TestResult{OK: true},
	}
	api := newTestAPI(t, WithSSHTester(fake))
	created := mustCreateServer(t, api)

	response := api.request(t, http.MethodPost, "/api/ssh/test", fmt.Sprintf(`{
		"server_id":%q,
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`, created.Server.ID))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	getResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID, "")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResponse.Code)
	}

	var fetched serverPayload
	decodeResponse(t, getResponse, &fetched)

	if fetched.Server.SavedPrivateKeyPath == nil || *fetched.Server.SavedPrivateKeyPath != "~/.ssh/proxy-node" {
		t.Fatalf("expected remembered private key path, got %#v", fetched.Server.SavedPrivateKeyPath)
	}
	if fake.request.PrivateKeyPath == nil || *fake.request.PrivateKeyPath != "~/.ssh/proxy-node" {
		t.Fatalf("expected forwarded private key path, got %#v", fake.request.PrivateKeyPath)
	}
}

func TestLatestSSHTestEndpointReturnsPersistedSuccessfulResult(t *testing.T) {
	fake := &fakeSSHTester{
		result: sshlayer.TestResult{
			OK: true,
			Facts: sshlayer.ServerFacts{
				Hostname:    "proxy-node-1",
				CurrentUser: "operator",
			},
			Commands: []sshlayer.CommandResult{{
				Name:     "hostname",
				Command:  "hostname",
				Stdout:   "proxy-node-1\n",
				ExitCode: 0,
				OK:       true,
			}},
		},
	}
	api := newTestAPI(t, WithSSHTester(fake))
	created := mustCreateServer(t, api)

	response := api.request(t, http.MethodPost, "/api/ssh/test", fmt.Sprintf(`{
		"server_id":%q,
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`, created.Server.ID))
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	latestResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/ssh-test/latest", "")
	if latestResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", latestResponse.Code, latestResponse.Body.String())
	}

	var payload latestSSHTestPayload
	decodeResponse(t, latestResponse, &payload)

	if payload.Result == nil || !payload.Result.OK {
		t.Fatalf("expected persisted ok ssh result, got %#v", payload.Result)
	}
	if payload.Result.Facts.Hostname != "proxy-node-1" {
		t.Fatalf("expected persisted hostname proxy-node-1, got %#v", payload.Result)
	}
	if payload.TestedAt == nil || payload.TestedAt.IsZero() {
		t.Fatalf("expected tested_at to be set, got %#v", payload.TestedAt)
	}
}

func TestLatestSSHTestEndpointReturnsPersistedFailure(t *testing.T) {
	fake := &fakeSSHTester{
		err: &sshlayer.OperationError{
			Kind:    sshlayer.ErrorKindAuth,
			Message: "permission denied",
		},
	}
	api := newTestAPI(t, WithSSHTester(fake))
	created := mustCreateServer(t, api)

	response := api.request(t, http.MethodPost, "/api/ssh/test", fmt.Sprintf(`{
		"server_id":%q,
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`, created.Server.ID))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d with body %s", response.Code, response.Body.String())
	}

	latestResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID+"/ssh-test/latest", "")
	if latestResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", latestResponse.Code, latestResponse.Body.String())
	}

	var payload latestSSHTestPayload
	decodeResponse(t, latestResponse, &payload)

	if payload.Result != nil {
		t.Fatalf("expected no persisted result for failed test, got %#v", payload.Result)
	}
	if payload.ErrorMessage != "permission denied" {
		t.Fatalf("expected persisted auth error, got %#v", payload)
	}
	if payload.TestedAt == nil || payload.TestedAt.IsZero() {
		t.Fatalf("expected tested_at to be set, got %#v", payload.TestedAt)
	}
}

func TestSSHDiscoverEndpointReturnsDetectedTelemtSettings(t *testing.T) {
	configText := strings.TrimSpace(`
[general]
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = false
tls = true

[general.links]
show = "*"
public_host = "mt.example.com"
public_port = 443

[server]
port = 443

[server.api]
enabled = true
listen = "0.0.0.0:9091"
whitelist = ["127.0.0.1/32"]

[access.users]
default = "0123456789abcdef0123456789abcdef"

[censorship]
tls_domain = "mt.example.com"
mask = true
mask_host = "www.yandex.ru"
mask_port = 443
tls_emulation = false
tls_front_dir = "tlsfront"
`) + "\n"
	stdout := fmt.Sprintf("remote_base_path=/srv/telemt\nconfig_path=/srv/telemt/config.toml\n%s\n%s%s\n", discoverConfigBeginMarker, configText, discoverConfigEndMarker)

	fake := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"discover_server_settings": okCommandResult(
				"discover_server_settings",
				"discover server settings",
				stdout,
			),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(fake))
	created := mustCreateServer(t, api)

	response := api.request(t, http.MethodPost, "/api/ssh/discover", fmt.Sprintf(`{
		"server_id":%q,
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`, created.Server.ID))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload discoverServerSettingsPayload
	decodeResponse(t, response, &payload)

	if payload.Discovery.RemoteBasePath != "/srv/telemt" {
		t.Fatalf("expected detected remote path, got %#v", payload.Discovery)
	}
	if payload.Discovery.ConfigPath != "/srv/telemt/config.toml" {
		t.Fatalf("expected detected config path, got %#v", payload.Discovery)
	}
	if strings.TrimRight(payload.Discovery.ConfigText, "\n") != strings.TrimRight(configText, "\n") {
		t.Fatalf("expected discovered config text, got %#v", payload.Discovery.ConfigText)
	}
	if payload.Discovery.PublicHost == nil || *payload.Discovery.PublicHost != "mt.example.com" {
		t.Fatalf("expected detected public host, got %#v", payload.Discovery.PublicHost)
	}
	if payload.Discovery.MTProtoPort == nil || *payload.Discovery.MTProtoPort != 443 {
		t.Fatalf("expected detected mtproto port, got %#v", payload.Discovery.MTProtoPort)
	}
	if payload.Discovery.Secret == nil || *payload.Discovery.Secret != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected detected secret, got %#v", payload.Discovery.Secret)
	}
	if payload.Discovery.SNIDomain == nil || *payload.Discovery.SNIDomain != "mt.example.com" {
		t.Fatalf("expected detected sni domain, got %#v", payload.Discovery.SNIDomain)
	}

	getResponse := api.request(t, http.MethodGet, "/api/servers/"+created.Server.ID, "")
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResponse.Code)
	}

	var fetched serverPayload
	decodeResponse(t, getResponse, &fetched)
	if fetched.Server.SavedPrivateKeyPath == nil || *fetched.Server.SavedPrivateKeyPath != "~/.ssh/proxy-node" {
		t.Fatalf("expected discovered request to remember path, got %#v", fetched.Server.SavedPrivateKeyPath)
	}
}

func TestBuildDiscoverServerSettingsCommandParsesTelemtConfig(t *testing.T) {
	remoteBasePath := t.TempDir()
	configPath := filepath.Join(remoteBasePath, "config.toml")
	configText := strings.TrimSpace(`
[general.links]
public_host = "mt.example.com"
public_port = 443

[access.users]
default = "0123456789abcdef0123456789abcdef"

[censorship]
tls_domain = "edge.example.com"
`) + "\n"
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	command := buildDiscoverServerSettingsCommand(&remoteBasePath)
	output, err := exec.Command("sh", "-lc", command).CombinedOutput()
	if err != nil {
		t.Fatalf("run discover command: %v\n%s", err, output)
	}

	parsed, parseErr := parseDiscoveredServerSettings(string(output))
	if parseErr != nil {
		t.Fatalf("parse discovered settings: %v\n%s", parseErr, output)
	}
	if parsed.RemoteBasePath != remoteBasePath {
		t.Fatalf("expected remote path %q, got %#v", remoteBasePath, parsed)
	}
	if parsed.ConfigPath != configPath {
		t.Fatalf("expected config path %q, got %#v", configPath, parsed)
	}
	if parsed.PublicHost == nil || *parsed.PublicHost != "mt.example.com" {
		t.Fatalf("expected public host, got %#v", parsed.PublicHost)
	}
	if parsed.MTProtoPort == nil || *parsed.MTProtoPort != 443 {
		t.Fatalf("expected port 443, got %#v", parsed.MTProtoPort)
	}
	if parsed.Secret == nil || *parsed.Secret != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected secret, got %#v", parsed.Secret)
	}
	if parsed.SNIDomain == nil || *parsed.SNIDomain != "edge.example.com" {
		t.Fatalf("expected sni domain, got %#v", parsed.SNIDomain)
	}
}

func TestDiscoverServerSettingsReadsConfigWithFallbackCommand(t *testing.T) {
	configText := strings.TrimSpace(`
[general]
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = false
tls = true

[general.links]
show = "*"
public_host = "mt.example.com"
public_port = 443

[server]
port = 443

[server.api]
enabled = true
listen = "127.0.0.1:9091"
whitelist = ["127.0.0.1/32"]

[access.users]
default = "0123456789abcdef0123456789abcdef"

[censorship]
tls_domain = "mt.example.com"
mask = true
mask_host = "www.yandex.ru"
mask_port = 443
tls_emulation = false
tls_front_dir = "tlsfront"
`) + "\n"
	stdout := strings.Join([]string{
		"remote_base_path=/srv/telemt",
		"config_path=/srv/telemt/config.toml",
		"public_host=mt.example.com",
		"mtproto_port=443",
		"secret=0123456789abcdef0123456789abcdef",
		"sni_domain=mt.example.com",
		"",
	}, "\n")

	fake := &fakeSSHExecutor{
		runResults: map[string]sshlayer.CommandResult{
			"discover_server_settings": okCommandResult(
				"discover_server_settings",
				"discover server settings",
				stdout,
			),
			"read_discovered_config": okCommandResult(
				"read_discovered_config",
				"cat /srv/telemt/config.toml",
				configText,
			),
		},
	}
	api := newTestAPI(t, WithSSHExecutor(fake))
	created := mustCreateServer(t, api)

	response := api.request(t, http.MethodPost, "/api/ssh/discover", fmt.Sprintf(`{
		"server_id":%q,
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"ssh_port":22,
		"auth_type":"private_key_path",
		"private_key_path":"~/.ssh/proxy-node"
	}`, created.Server.ID))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload discoverServerSettingsPayload
	decodeResponse(t, response, &payload)
	if strings.TrimRight(payload.Discovery.ConfigText, "\n") != strings.TrimRight(configText, "\n") {
		t.Fatalf("expected fallback config text, got %#v", payload.Discovery.ConfigText)
	}
	if payload.Discovery.PublicHost == nil || *payload.Discovery.PublicHost != "mt.example.com" {
		t.Fatalf("expected discovered public host, got %#v", payload.Discovery.PublicHost)
	}
	if _, ok := fake.runRequests["read_discovered_config"]; !ok {
		t.Fatalf("expected fallback read command to run, got %#v", fake.runRequests)
	}
}

func TestBuildDiscoverServerSettingsCommandFallsBackToHomeTelemt(t *testing.T) {
	homeDir := t.TempDir()
	remoteBasePath := filepath.Join(homeDir, "telemt")
	configPath := filepath.Join(remoteBasePath, "config.toml")
	if err := os.MkdirAll(remoteBasePath, 0o755); err != nil {
		t.Fatalf("mkdir telemt home: %v", err)
	}

	configText := strings.TrimSpace(`
[general]
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = false
tls = true

[general.links]
show = "*"
public_host = "home.example.com"
public_port = 8443

[server]
port = 443

[server.api]
enabled = true
listen = "0.0.0.0:9091"
whitelist = ["127.0.0.1/32"]

[access.users]
default = "fedcba9876543210fedcba9876543210"

[censorship]
tls_domain = "home.example.com"
mask = true
mask_host = "www.yandex.ru"
mask_port = 443
tls_emulation = false
tls_front_dir = "tlsfront"

`) + "\n"
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatalf("write home config: %v", err)
	}

	hint := filepath.Join(t.TempDir(), "missing-telemt")
	command := buildDiscoverServerSettingsCommand(&hint)
	cmd := exec.Command("sh", "-lc", command)
	cmd.Env = append(os.Environ(), "HOME="+homeDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run discover command: %v\n%s", err, output)
	}

	parsed, parseErr := parseDiscoveredServerSettings(string(output))
	if parseErr != nil {
		t.Fatalf("parse discovered settings: %v\n%s", parseErr, output)
	}
	if parsed.RemoteBasePath != remoteBasePath {
		t.Fatalf("expected home telemt path %q, got %#v", remoteBasePath, parsed)
	}
	if parsed.ConfigPath != configPath {
		t.Fatalf("expected config path %q, got %#v", configPath, parsed)
	}
	if parsed.PublicHost == nil || *parsed.PublicHost != "home.example.com" {
		t.Fatalf("expected home public host, got %#v", parsed.PublicHost)
	}
	if parsed.MTProtoPort == nil || *parsed.MTProtoPort != 8443 {
		t.Fatalf("expected home port 8443, got %#v", parsed.MTProtoPort)
	}
	if parsed.Secret == nil || *parsed.Secret != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("expected home secret, got %#v", parsed.Secret)
	}
	if parsed.SNIDomain == nil || *parsed.SNIDomain != "home.example.com" {
		t.Fatalf("expected home sni domain, got %#v", parsed.SNIDomain)
	}
}

func TestBuildDiscoverServerSettingsCommandSearchesKnownTelemtPaths(t *testing.T) {
	command := buildDiscoverServerSettingsCommand(nil)

	if !strings.Contains(command, `"/srv/telemt"`) {
		t.Fatalf("expected discovery command to scan /srv/telemt, got %q", command)
	}
	if !strings.Contains(command, `"${HOME}/telemt"`) {
		t.Fatalf("expected discovery command to scan ${HOME}/telemt, got %q", command)
	}
	if !strings.Contains(command, `"/opt/mtproto-panel/telemt"`) {
		t.Fatalf("expected discovery command to scan /opt/mtproto-panel/telemt, got %q", command)
	}
}

func TestSSHTestEndpointValidationError(t *testing.T) {
	fake := &fakeSSHTester{
		err: &sshlayer.ValidationError{Fields: map[string]string{
			"host": "is required",
		}},
	}
	api := newTestAPI(t, WithSSHTester(fake))

	response := api.request(t, http.MethodPost, "/api/ssh/test", `{
		"host":"",
		"ssh_user":"operator",
		"auth_type":"private_key_text",
		"private_key_text":"key"
	}`)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d with body %s", response.Code, response.Body.String())
	}

	var payload errorPayload
	decodeResponse(t, response, &payload)
	if payload.Error.Code != "validation_error" {
		t.Fatalf("expected validation_error, got %q", payload.Error.Code)
	}
	if payload.Error.Details["host"] != "is required" {
		t.Fatalf("expected host validation error, got %#v", payload.Error.Details)
	}
}

func TestSSHTestEndpointTimeoutError(t *testing.T) {
	fake := &fakeSSHTester{
		err: &sshlayer.OperationError{Kind: sshlayer.ErrorKindTimeout, Message: "ssh connection timed out"},
	}
	api := newTestAPI(t, WithSSHTester(fake))

	response := api.request(t, http.MethodPost, "/api/ssh/test", `{
		"host":"203.0.113.10",
		"ssh_user":"operator",
		"auth_type":"private_key_text",
		"private_key_text":"key"
	}`)

	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504, got %d with body %s", response.Code, response.Body.String())
	}

	var payload errorPayload
	decodeResponse(t, response, &payload)
	if payload.Error.Code != "ssh_timeout" {
		t.Fatalf("expected ssh_timeout, got %q", payload.Error.Code)
	}
}

type testAPI struct {
	handler http.Handler
	db      *database.DB
}

func newTestAPI(t *testing.T, options ...Option) *testAPI {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "panel.db")
	db, err := database.Open(databasePath)
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

	return &testAPI{
		handler: NewWithOptions(db, options...).Handler(),
		db:      db,
	}
}

func (a *testAPI) request(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if strings.TrimSpace(body) != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func quoteValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func mustCreateServer(t *testing.T, api *testAPI) serverPayload {
	t.Helper()

	return mustCreateServerWithValues(t, api, "proxy_node_1", "203.0.113.10")
}

func mustCreateServerWithValues(t *testing.T, api *testAPI, name, host string) serverPayload {
	t.Helper()

	response := api.request(t, http.MethodPost, "/api/servers", `{
		"name":"`+name+`",
		"host":"`+host+`",
		"ssh_user":"operator",
		"public_host":"mt.example.com"
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d with body %s", response.Code, response.Body.String())
	}

	var created serverPayload
	decodeResponse(t, response, &created)
	return created
}

func assertRelationshipView(t *testing.T, relationships []serverRelationshipView, relationshipType, direction, peerServerID string) {
	t.Helper()

	for _, relationship := range relationships {
		if relationship.Type == relationshipType && relationship.Direction == direction && relationship.PeerServerID == peerServerID {
			return
		}
	}

	t.Fatalf("expected relationship type=%s direction=%s peer=%s in %#v", relationshipType, direction, peerServerID, relationships)
}

func mustGenerateConfig(t *testing.T, api *testAPI, serverID string) configStatePayload {
	t.Helper()

	response := api.request(t, http.MethodPost, "/api/servers/"+serverID+"/configs/generate", `{
		"public_host":"mt.example.com"
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected generate status 200, got %d with body %s", response.Code, response.Body.String())
	}

	var payload configStatePayload
	decodeResponse(t, response, &payload)
	return payload
}

type fakeSSHTester struct {
	result  sshlayer.TestResult
	err     error
	request sshlayer.TestRequest
}

func (f *fakeSSHTester) Test(_ context.Context, request sshlayer.TestRequest) (sshlayer.TestResult, error) {
	f.request = request
	return f.result, f.err
}

type fakeSSHExecutor struct {
	runResults    map[string]sshlayer.CommandResult
	runErrors     map[string]error
	runRequests   map[string]sshlayer.CommandRequest
	uploadResults map[string]sshlayer.CommandResult
	uploadErrors  map[string]error
	calls         []string
	lastRequest   sshlayer.TestRequest
	lastUpload    sshlayer.UploadRequest
}

type fakeTelegramAlertSender struct {
	messages []string
}

func (f *fakeTelegramAlertSender) Send(_ context.Context, _, _, text string) error {
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeSSHExecutor) Run(_ context.Context, request sshlayer.TestRequest, command sshlayer.CommandRequest) (sshlayer.CommandResult, error) {
	f.lastRequest = request
	f.calls = append(f.calls, "run:"+command.Name)
	if f.runRequests == nil {
		f.runRequests = map[string]sshlayer.CommandRequest{}
	}
	f.runRequests[command.Name] = command
	if err := f.runErrors[command.Name]; err != nil {
		return sshlayer.CommandResult{}, err
	}
	result, ok := f.runResults[command.Name]
	if !ok {
		return okCommandResult(command.Name, command.Command, ""), nil
	}
	if result.Name == "" {
		result.Name = command.Name
	}
	if result.Command == "" {
		result.Command = command.Command
	}
	return result, nil
}

func (f *fakeSSHExecutor) Upload(_ context.Context, request sshlayer.TestRequest, upload sshlayer.UploadRequest) (sshlayer.CommandResult, error) {
	f.lastRequest = request
	f.lastUpload = upload
	f.calls = append(f.calls, "upload:"+upload.Name)
	if err := f.uploadErrors[upload.Name]; err != nil {
		return sshlayer.CommandResult{}, err
	}
	result, ok := f.uploadResults[upload.Name]
	if !ok {
		return okCommandResult(upload.Name, upload.RemotePath, ""), nil
	}
	if result.Name == "" {
		result.Name = upload.Name
	}
	if result.Command == "" {
		result.Command = upload.RemotePath
	}
	return result, nil
}

func okCommandResult(name, command, stdout string) sshlayer.CommandResult {
	return sshlayer.CommandResult{
		Name:       name,
		Command:    command,
		Stdout:     stdout,
		ExitCode:   0,
		DurationMS: 5,
		OK:         true,
	}
}
