package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Notifier defines the interface for sending notifications
type Notifier interface {
	Notify(park string, arrival string, results map[string][]string) error
}

// DiscordNotifier implements Notifier for Discord Webhooks
type DiscordNotifier struct {
	WebhookURL string
	Logger     *slog.Logger
}

type discordPayload struct {
	Content string  `json:"content"`
	Embeds  []embed `json:"embeds"`
}

type embed struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Color       int     `json:"color"`
	Fields      []field `json:"fields"`
	Timestamp   string  `json:"timestamp"`
}

type field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

func NewDiscordNotifier(url string, logger *slog.Logger) *DiscordNotifier {
	return &DiscordNotifier{
		WebhookURL: url,
		Logger:     logger,
	}
}

func (d *DiscordNotifier) Notify(park string, arrival string, results map[string][]string) error {
	if d.WebhookURL == "" {
		return nil
	}

	d.Logger.Info("Sending Discord notification", slog.String("park", park))

	var fields []field
	for campground, campsites := range results {
		val := fmt.Sprintf("%d campsites found", len(campsites))
		if len(campsites) > 9 {
			val = "9+ campsites found"
		}
		fields = append(fields, field{
			Name:   campground,
			Value:  val,
			Inline: true,
		})
	}

	payload := discordPayload{
		Content: fmt.Sprintf("🏕️ **New Availability Found!**\n**Park:** %s\n**Arrival:** %s", park, arrival),
		Embeds: []embed{
			{
				Title:       "Availability Details",
				Description: fmt.Sprintf("Found availability in %d campgrounds.", len(results)),
				Color:       3066993, // Green
				Fields:      fields,
				Timestamp:   time.Now().Format(time.RFC3339),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("could not marshal discord payload: %w", err)
	}

	resp, err := http.Post(d.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("could not send discord notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord API returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}
