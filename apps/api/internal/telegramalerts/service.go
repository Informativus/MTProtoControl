package telegramalerts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const telegramAPIBaseURL = "https://api.telegram.org"

type Sender interface {
	Send(ctx context.Context, botToken, chatID, text string) error
}

type Service struct {
	repo   *Repository
	sender Sender
	now    func() time.Time
}

type SendError struct {
	Message string
}

func (e *SendError) Error() string {
	return e.Message
}

type HTTPSender struct {
	baseURL string
	client  *http.Client
}

func NewService(repo *Repository, sender Sender) *Service {
	if sender == nil {
		sender = NewHTTPSender(nil)
	}
	return &Service{
		repo:   repo,
		sender: sender,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func NewHTTPSender(client *http.Client) *HTTPSender {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &HTTPSender{
		baseURL: telegramAPIBaseURL,
		client:  client,
	}
}

func (s *Service) Load(ctx context.Context) (Settings, error) {
	if s == nil || s.repo == nil {
		return Settings{RepeatDownAfterMinutes: defaultRepeatDownAfterMinutes}, nil
	}
	return s.repo.Load(ctx)
}

func (s *Service) PublicSettings(ctx context.Context) (PublicSettings, error) {
	settings, err := s.Load(ctx)
	if err != nil {
		return PublicSettings{}, err
	}
	return settings.Public(), nil
}

func (s *Service) UpdateSettings(ctx context.Context, input UpdateInput) (PublicSettings, error) {
	if s == nil || s.repo == nil {
		return PublicSettings{}, fmt.Errorf("telegram alerts service is not configured")
	}
	settings, err := s.repo.Save(ctx, input)
	if err != nil {
		return PublicSettings{}, err
	}
	return settings.Public(), nil
}

func (s *Service) SendTestAlert(ctx context.Context) error {
	settings, err := s.Load(ctx)
	if err != nil {
		return err
	}
	return s.SendWithSettings(ctx, settings, buildTestMessage(s.now()), false)
}

func (s *Service) SendWithSettings(ctx context.Context, settings Settings, text string, requireEnabled bool) error {
	if s == nil || s.sender == nil {
		return fmt.Errorf("telegram alerts service is not configured")
	}
	if err := validateSendSettings(settings, requireEnabled); err != nil {
		return err
	}
	return s.sender.Send(ctx, settings.TelegramBotToken, settings.TelegramChatID, text)
}

func (s *HTTPSender) Send(ctx context.Context, botToken, chatID, text string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("telegram HTTP sender is not configured")
	}

	endpoint := strings.TrimRight(s.baseURL, "/") + "/bot" + strings.TrimSpace(botToken) + "/sendMessage"
	form := url.Values{}
	form.Set("chat_id", strings.TrimSpace(chatID))
	form.Set("text", text)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := s.client.Do(req)
	if err != nil {
		return &SendError{Message: fmt.Sprintf("telegram request failed: %s", strings.TrimSpace(err.Error()))}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return &SendError{Message: fmt.Sprintf("read telegram response: %s", strings.TrimSpace(err.Error()))}
	}

	var payload struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &payload)

	if response.StatusCode != http.StatusOK {
		message := firstNonEmpty(payload.Description, strings.TrimSpace(string(body)), response.Status)
		return &SendError{Message: fmt.Sprintf("telegram API returned HTTP %d: %s", response.StatusCode, message)}
	}
	if !payload.OK {
		return &SendError{Message: firstNonEmpty(payload.Description, "telegram API returned ok=false")}
	}

	return nil
}

func validateSendSettings(settings Settings, requireEnabled bool) error {
	validationErr := &ValidationError{}
	if requireEnabled && !settings.AlertsEnabled {
		validationErr.add("alerts_enabled", "must be enabled before alerts can be sent automatically")
	}
	if strings.TrimSpace(settings.TelegramBotToken) == "" {
		validationErr.add("telegram_bot_token", "is required")
	}
	if strings.TrimSpace(settings.TelegramChatID) == "" {
		validationErr.add("telegram_chat_id", "is required")
	}
	if validationErr.empty() {
		return nil
	}
	return validationErr
}

func buildTestMessage(now time.Time) string {
	return strings.Join([]string{
		"MTProto alert test",
		"Panel: MTProxy Control",
		"Time: " + now.UTC().Format("2006-01-02 15:04:05 MST"),
	}, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
