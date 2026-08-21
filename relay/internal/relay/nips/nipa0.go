package nips

import (
	"fmt"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-A0: Voice Messages
// https://github.com/nostr-protocol/nips/blob/master/A0.md
//
// NIP-A0 defines event kinds for short voice messages (typically up to 60 seconds).
//
// Event kinds:
//   - 1222: Root voice message
//   - 1244: Reply voice message (follows NIP-22 comment structure)
//
// Content MUST be a URL pointing directly to an audio file.
// Recommended format: audio/mp4 (.m4a) with AAC or Opus encoding.

// VoiceMessageKind is the kind for root voice messages.
const VoiceMessageKind = 1222

// VoiceMessageReplyKind is the kind for reply voice messages.
const VoiceMessageReplyKind = 1244

// RecommendedVoiceFormats lists recommended audio MIME types.
var RecommendedVoiceFormats = map[string]bool{
	"audio/mp4":  true,
	"audio/ogg":  true,
	"audio/webm": true,
	"audio/mpeg": true,
}

// MaxVoiceDurationSeconds is the recommended max duration per spec.
const MaxVoiceDurationSeconds = 60

// ValidateVoiceMessageContent validates the content (URL) of a voice message.
func ValidateVoiceMessageContent(content string) error {
	if content == "" {
		return fmt.Errorf("voice message content cannot be empty")
	}
	if !strings.HasPrefix(content, "http://") && !strings.HasPrefix(content, "https://") {
		return fmt.Errorf("voice message content must be an HTTP(S) URL: %s", content)
	}
	return nil
}

// GetVoiceDuration returns the duration from the imeta tag, or 0 if not present.
func GetVoiceDuration(evt *nostr.Event) int {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "imeta" {
			for _, part := range tag[1:] {
				if strings.HasPrefix(part, "duration ") {
					var duration int
					_, err := fmt.Sscanf(part, "duration %d", &duration)
					if err == nil {
						return duration
					}
				}
			}
		}
	}
	return 0
}

// GetVoiceWaveform returns the waveform values from the imeta tag.
func GetVoiceWaveform(evt *nostr.Event) []int {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "imeta" {
			for _, part := range tag[1:] {
				if strings.HasPrefix(part, "waveform ") {
					valuesStr := strings.TrimPrefix(part, "waveform ")
					values := strings.Fields(valuesStr)
					waveform := make([]int, 0, len(values))
					for _, v := range values {
						var n int
						if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
							waveform = append(waveform, n)
						}
					}
					return waveform
				}
			}
		}
	}
	return nil
}

// ValidateNIPA0Event validates a NIP-A0 voice message event.
func ValidateNIPA0Event(evt *nostr.Event) error {
	if evt.Kind != VoiceMessageKind && evt.Kind != VoiceMessageReplyKind {
		return fmt.Errorf("not a NIP-A0 event kind: %d", evt.Kind)
	}
	if err := ValidateVoiceMessageContent(evt.Content); err != nil {
		return err
	}
	return nil
}
