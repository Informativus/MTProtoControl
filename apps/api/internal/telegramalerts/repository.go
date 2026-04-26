package telegramalerts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
)

const (
	settingTelegramBotToken        = "telegram_bot_token"
	settingTelegramChatID          = "telegram_chat_id"
	settingAlertsEnabled           = "alerts_enabled"
	settingRepeatDownAfterMinutes  = "repeat_down_after_minutes"
	defaultRepeatDownAfterMinutes  = 0
	EventTypeAlertOffline          = "telegram_alert_offline"
	EventTypeAlertDegraded         = "telegram_alert_degraded"
	EventTypeAlertRecovered        = "telegram_alert_recovered"
	EventTypeAlertDeployFailed     = "telegram_alert_deploy_failed"
	EventTypeAlertTest             = "telegram_alert_test"
	EventTypeAlertOfflineRepeated  = "telegram_alert_offline_repeated"
	EventTypeAlertDegradedRepeated = "telegram_alert_degraded_repeated"
)

type Repository struct {
	db *database.DB
}

type Settings struct {
	TelegramBotToken       string
	TelegramChatID         string
	AlertsEnabled          bool
	RepeatDownAfterMinutes int
}

type PublicSettings struct {
	TelegramBotTokenConfigured bool   `json:"telegram_bot_token_configured"`
	TelegramBotTokenMasked     string `json:"telegram_bot_token_masked,omitempty"`
	TelegramChatID             string `json:"telegram_chat_id"`
	AlertsEnabled              bool   `json:"alerts_enabled"`
	RepeatDownAfterMinutes     int    `json:"repeat_down_after_minutes"`
}

type UpdateInput struct {
	TelegramBotToken       *string
	TelegramChatID         *string
	AlertsEnabled          *bool
	RepeatDownAfterMinutes *int
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "validation failed"
}

func (e *ValidationError) add(field, message string) {
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	if _, exists := e.Fields[field]; !exists {
		e.Fields[field] = message
	}
}

func (e *ValidationError) empty() bool {
	return len(e.Fields) == 0
}

type settingRow struct {
	Key        string  `json:"key"`
	ValuePlain *string `json:"value_plain"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Load(ctx context.Context) (Settings, error) {
	settings := Settings{
		RepeatDownAfterMinutes: defaultRepeatDownAfterMinutes,
	}
	if r == nil || r.db == nil {
		return settings, nil
	}

	var rows []settingRow
	if err := r.db.Query(ctx, script(
		`SELECT key, value_plain
		 FROM app_settings
		 WHERE key IN (
		   'telegram_bot_token',
		   'telegram_chat_id',
		   'alerts_enabled',
		   'repeat_down_after_minutes'
		 );`,
	), &rows); err != nil {
		return Settings{}, fmt.Errorf("load telegram settings: %w", err)
	}

	for _, row := range rows {
		value := ""
		if row.ValuePlain != nil {
			value = strings.TrimSpace(*row.ValuePlain)
		}

		switch row.Key {
		case settingTelegramBotToken:
			settings.TelegramBotToken = value
		case settingTelegramChatID:
			settings.TelegramChatID = value
		case settingAlertsEnabled:
			settings.AlertsEnabled = value == "1" || strings.EqualFold(value, "true")
		case settingRepeatDownAfterMinutes:
			if value == "" {
				continue
			}
			minutes, err := strconv.Atoi(value)
			if err != nil {
				return Settings{}, fmt.Errorf("parse repeat_down_after_minutes: %w", err)
			}
			settings.RepeatDownAfterMinutes = minutes
		}
	}

	return settings, nil
}

func (r *Repository) Save(ctx context.Context, input UpdateInput) (Settings, error) {
	current, err := r.Load(ctx)
	if err != nil {
		return Settings{}, err
	}

	if input.TelegramBotToken != nil {
		current.TelegramBotToken = strings.TrimSpace(*input.TelegramBotToken)
	}
	if input.TelegramChatID != nil {
		current.TelegramChatID = strings.TrimSpace(*input.TelegramChatID)
	}
	if input.AlertsEnabled != nil {
		current.AlertsEnabled = *input.AlertsEnabled
	}
	if input.RepeatDownAfterMinutes != nil {
		current.RepeatDownAfterMinutes = *input.RepeatDownAfterMinutes
	}

	if err := validateSettings(current, true); err != nil {
		return Settings{}, err
	}

	if r == nil || r.db == nil {
		return current, nil
	}

	updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@token_key", settingTelegramBotToken),
		setParam("@token_value", current.TelegramBotToken),
		setParam("@chat_id_key", settingTelegramChatID),
		setParam("@chat_id_value", current.TelegramChatID),
		setParam("@enabled_key", settingAlertsEnabled),
		setParam("@enabled_value", boolString(current.AlertsEnabled)),
		setParam("@repeat_key", settingRepeatDownAfterMinutes),
		setParam("@repeat_value", strconv.Itoa(current.RepeatDownAfterMinutes)),
		setParam("@updated_at", updatedAt),
		upsertSettingSQL("@token_key", "@token_value"),
		upsertSettingSQL("@chat_id_key", "@chat_id_value"),
		upsertSettingSQL("@enabled_key", "@enabled_value"),
		upsertSettingSQL("@repeat_key", "@repeat_value"),
	)); err != nil {
		return Settings{}, fmt.Errorf("save telegram settings: %w", err)
	}

	return current, nil
}

func (s Settings) Public() PublicSettings {
	public := PublicSettings{
		TelegramChatID:         s.TelegramChatID,
		AlertsEnabled:          s.AlertsEnabled,
		RepeatDownAfterMinutes: s.RepeatDownAfterMinutes,
	}
	if strings.TrimSpace(s.TelegramBotToken) != "" {
		public.TelegramBotTokenConfigured = true
		public.TelegramBotTokenMasked = maskToken(s.TelegramBotToken)
	}
	return public
}

func validateSettings(settings Settings, requireReadyWhenEnabled bool) error {
	validationErr := &ValidationError{}
	if settings.RepeatDownAfterMinutes < 0 {
		validationErr.add("repeat_down_after_minutes", "must be zero or greater")
	}
	if requireReadyWhenEnabled && settings.AlertsEnabled {
		if strings.TrimSpace(settings.TelegramBotToken) == "" {
			validationErr.add("telegram_bot_token", "is required when alerts are enabled")
		}
		if strings.TrimSpace(settings.TelegramChatID) == "" {
			validationErr.add("telegram_chat_id", "is required when alerts are enabled")
		}
	}
	if validationErr.empty() {
		return nil
	}
	return validationErr
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func maskToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return "configured"
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}

func upsertSettingSQL(keyParam, valueParam string) string {
	return fmt.Sprintf(`INSERT INTO app_settings (key, value_encrypted, value_plain, updated_at)
		VALUES (%s, null, %s, @updated_at)
		ON CONFLICT(key) DO UPDATE SET
		  value_encrypted = null,
		  value_plain = excluded.value_plain,
		  updated_at = excluded.updated_at;`, keyParam, valueParam)
}

func script(lines ...string) string {
	return strings.Join(lines, "\n")
}

func parameterInit() string {
	return ".parameter init"
}

func setParam(name, value string) string {
	return fmt.Sprintf(".parameter set %s %s", name, quote(value))
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
