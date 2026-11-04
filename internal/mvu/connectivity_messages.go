package mvu

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// ConnectedMessage is the successful connection status shown by host and attach.
const ConnectedMessage = "connected"

// ConnectionLostMessage formats a persistent reconnect status banner message.
func ConnectionLostMessage(endpoint string) string {
	label := strings.TrimSpace(endpoint)
	if label == "" {
		return "connection lost, reconnecting"
	}
	return fmt.Sprintf("connection lost to %s, reconnecting", label)
}

// ConnectionLostBackoffMessage formats a reconnect countdown status banner message.
func ConnectionLostBackoffMessage(endpoint string, remaining time.Duration) string {
	seconds := int(math.Ceil(remaining.Seconds()))
	if seconds < 0 {
		seconds = 0
	}
	label := strings.TrimSpace(endpoint)
	if label == "" {
		return fmt.Sprintf("connection lost, reconnecting in %ds", seconds)
	}
	return fmt.Sprintf("connection lost to %s, reconnecting in %ds", label, seconds)
}
