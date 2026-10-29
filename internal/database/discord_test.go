package database

import "testing"

func TestParseDiscordWebhook(t *testing.T) {
	const token = "AbC-123_xyz"
	for _, raw := range []string{
		"https://discord.com/api/webhooks/123456789012345678/" + token,
		" https://discordapp.com/api/webhooks/123456789012345678/" + token + " ",
		"https://canary.discord.com/api/webhooks/123456789012345678/" + token,
		"https://ptb.discord.com/api/v10/webhooks/123456789012345678/" + token + "/",
	} {
		id, got, err := ParseDiscordWebhook(raw)
		if err != nil || id != "123456789012345678" || got != token {
			t.Errorf("%q: %q %q %v", raw, id, got, err)
		}
	}
	for _, raw := range []string{
		"",
		"http://discord.com/api/webhooks/1/" + token,
		"https://discord.com.evil.example/api/webhooks/1/" + token,
		"https://evil.example/api/webhooks/1/" + token,
		"https://discord.com:8443/api/webhooks/1/" + token,
		"https://user:pass@discord.com/api/webhooks/1/" + token,
		"https://discord.com/api/webhooks/1/" + token + "?wait=true",
		"https://discord.com/api/webhooks/1/" + token + "#x",
		"https://discord.com/api/webhooks/abc/" + token,
		"https://discord.com/api/webhooks/1/" + token + "/extra",
		"https://discord.com/api/webhooks/1/tok%2F..%2Fen",
		"https://discord.com/api/channels/1/messages",
		"https://Discord.com/api/webhooks/1/" + token,
		"https://127.0.0.1/api/webhooks/1/" + token,
	} {
		if _, _, err := ParseDiscordWebhook(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
