// Package notify pushes one-way alerts to a Telegram group via the Bot API.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Notifier sends a short alert. Implementations must not block for long.
type Notifier interface {
	Notify(ctx context.Context, key, text string)
}

// Nop drops every message.
type Nop struct{}

// Notify implements Notifier.
func (Nop) Notify(context.Context, string, string) {}

// Telegram sends messages with sendMessage. Messages with the same key are
// suppressed for Cooldown so a flapping VPN does not spam the group.
type Telegram struct {
	Token    string
	ChatID   string
	BaseURL  string // default https://api.telegram.org
	Client   *http.Client
	Cooldown time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

// NewTelegram returns a Telegram notifier, or Nop if token or chat is empty.
func NewTelegram(token, chatID string) Notifier {
	if token == "" || chatID == "" {
		return Nop{}
	}
	return &Telegram{Token: token, ChatID: chatID, Client: &http.Client{Timeout: 10 * time.Second}, Cooldown: 10 * time.Minute}
}

// Notify implements Notifier; it sends asynchronously and logs failures.
func (t *Telegram) Notify(ctx context.Context, key, text string) {
	if !t.allow(key) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := t.send(ctx, text); err != nil {
			slog.Warn("telegram notify failed", "key", key, "err", err)
		}
	}()
}

func (t *Telegram) allow(key string) bool {
	if key == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = map[string]time.Time{}
	}
	if at, ok := t.last[key]; ok && time.Since(at) < t.Cooldown {
		return false
	}
	t.last[key] = time.Now()
	return true
}

// Reset clears the cooldown for key (e.g. after the condition recovered).
func (t *Telegram) Reset(key string) {
	t.mu.Lock()
	delete(t.last, key)
	t.mu.Unlock()
}

func (t *Telegram) send(ctx context.Context, text string) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	form := url.Values{"chat_id": {t.ChatID}, "text": {text}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+t.Token+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.Client.Do(req)
	if err != nil {
		// The URL contains the token; never log it.
		return fmt.Errorf("request failed: %w", stripURL(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &r)
	if resp.StatusCode != http.StatusOK || !r.OK {
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, r.Description)
	}
	return nil
}

func stripURL(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}

// Resetter is implemented by notifiers that support clearing a cooldown.
type Resetter interface{ Reset(key string) }
