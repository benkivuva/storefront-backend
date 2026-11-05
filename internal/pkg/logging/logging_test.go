package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(NewJSON(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestSensitiveKeys_Redacted(t *testing.T) {
	keys := []string{
		"password", "password_hash", "new_password", "old_password",
		"token", "access_token", "refresh_token", "id_token",
		"authorization", "api_key", "secret", "client_secret", "private_key",
		"phone", "phone_e164", "msisdn",
		"card_number", "pan", "cvv", "cvc", "pin", "otp",
		"csrf_token", "session", "cookie", "set_cookie",
		"raw_body", "webhook_body", "raw_webhook",
	}
	for _, k := range keys {
		t.Run(k, func(t *testing.T) {
			logger, buf := newTestLogger(t)
			logger.Info("m", k, "hunter2")
			if strings.Contains(buf.String(), "hunter2") {
				t.Fatalf("leaked: %s", buf.String())
			}
			if !strings.Contains(buf.String(), redacted) {
				t.Fatalf("not redacted: %s", buf.String())
			}
		})
	}
}

func TestSensitiveKeys_CaseInsensitive(t *testing.T) {
	for _, k := range []string{"Password", "PASSWORD", "PassWord", "API_KEY", "Authorization"} {
		t.Run(k, func(t *testing.T) {
			logger, buf := newTestLogger(t)
			logger.Info("m", k, "hunter2")
			if strings.Contains(buf.String(), "hunter2") {
				t.Fatalf("leaked: %s", buf.String())
			}
		})
	}
}

func TestNonSensitiveKeys_PassThrough(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.Info("m", "request_id", "abc-123", "user_id", "user-uuid")
	out := buf.String()
	if !strings.Contains(out, "abc-123") || !strings.Contains(out, "user-uuid") {
		t.Fatalf("dropped: %s", out)
	}
	if strings.Contains(out, redacted) {
		t.Fatalf("false positive: %s", out)
	}
}

func TestGroupedAttributes_Redacted(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.Info("m", slog.Group("auth", "token", "hunter2", "user_id", "u1"))
	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("grouped token leaked: %s", out)
	}
	if !strings.Contains(out, "u1") {
		t.Fatalf("grouped non-sensitive dropped: %s", out)
	}
}

func TestWithAttrs_Redacted(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.With("password", "hunter2").Info("m")
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("With() leaked: %s", buf.String())
	}
}

func TestWithGroupedAttrs_Redacted(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.With(slog.Group("auth", "token", "hunter2")).Info("m")
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("With(Group) leaked: %s", buf.String())
	}
}

func TestWithGroup_Redacted(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.WithGroup("auth").With("token", "hunter2").Info("m")
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("WithGroup leaked: %s", buf.String())
	}
}

func TestLogAttrs_Redacted(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "m",
		slog.String("password", "hunter2"))
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("LogAttrs leaked: %s", buf.String())
	}
}

func TestMessageAndLevelPreserved(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.Warn("interesting", "password", "hunter2")
	out := buf.String()
	if !strings.Contains(out, `"msg":"interesting"`) {
		t.Fatalf("msg lost: %s", out)
	}
	if !strings.Contains(out, `"level":"WARN"`) {
		t.Fatalf("level lost: %s", out)
	}
}

func TestNonStringValue_Redacted(t *testing.T) {
	logger, buf := newTestLogger(t)
	logger.Info("m", "password", 12345)
	out := buf.String()
	if strings.Contains(out, "12345") {
		t.Fatalf("non-string leaked: %s", out)
	}
	if !strings.Contains(out, redacted) {
		t.Fatalf("not redacted: %s", out)
	}
}
