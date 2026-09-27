package provider

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestZhipuTermOf(t *testing.T) {
	var subs []zhipuSubscription
	json.Unmarshal([]byte(`[
		{"status":"EXPIRED","nextRenewTime":"2026-08-01 00:00:00","autoRenew":1},
		{"status":"VALID","valid":"2026-09-18 10:00:00-2026-10-18 10:00:00","autoRenew":1,"nextRenewTime":"2026-10-18 10:00:00"}]`), &subs)
	until, renew := zhipuTermOf(subs)
	if until == nil || renew != "auto" || !until.Equal(time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("auto: %v %q", until, renew)
	}
	subs = nil
	json.Unmarshal([]byte(`[{"status":"VALID","valid":"2026-09-18 10:00:00-2026-12-18 10:00:00","autoRenew":false}]`), &subs)
	if until, renew = zhipuTermOf(subs); until == nil || renew != "off" || until.Month() != 12 {
		t.Fatalf("off, from valid: %v %q", until, renew)
	}
	subs = nil
	json.Unmarshal([]byte(`[{"status":"VALID","autoRenew":true}]`), &subs)
	if until, renew = zhipuTermOf(subs); until != nil || renew != "" {
		t.Fatalf("nothing said: %v %q", until, renew)
	}
}

func TestCodexUntil(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	auth := func(until string) []byte {
		claims, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_subscription_active_until": until}})
		b, _ := json.Marshal(map[string]any{"tokens": map[string]any{"id_token": "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"}})
		return b
	}
	if u := codexUntil(auth("2026-10-18T09:00:00+00:00"), now); u == nil || u.Day() != 18 {
		t.Fatalf("future: %v", u)
	}
	if u := codexUntil(auth("2026-09-18T09:00:00+00:00"), now); u != nil {
		t.Fatalf("a stale token's date is shown: %v", u)
	}
	if u := codexUntil([]byte(`{}`), now); u != nil {
		t.Fatalf("none: %v", u)
	}
}
