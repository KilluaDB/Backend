package handler

import (
	"backend/internal/response"
	"errors"
	"log"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgFail returns a consistent error response for Postgres endpoints:
// - includes a sanitized error string (for DBaaS UX)
// - optionally includes SQLSTATE code when available
func pgFail(c *gin.Context, statusCode int, err error, message string) {
	resp := response.APIResponse{
		Status:  "error",
		Message: message,
	}

	if err != nil {
		log.Printf("Error: %v", err)
		resp.Error = sanitizePgErrorString(err.Error())

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			resp.Code = pgErr.Code
		}
	}

	c.JSON(statusCode, resp)
}

// pgFailWithData is like pgFail, but includes a structured payload in "data".
// Useful when we want consistent error shape while returning partial execution results.
func pgFailWithData(c *gin.Context, statusCode int, err error, message string, data any) {
	resp := response.APIResponse{
		Status:  "error",
		Message: message,
		Data:    data,
	}

	if err != nil {
		log.Printf("Error: %v", err)
		resp.Error = sanitizePgErrorString(err.Error())

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			resp.Code = pgErr.Code
		}
	}

	c.JSON(statusCode, resp)
}

var (
	// Very conservative scrubbing: hide obvious credential/DSN pieces and internal network identifiers.
	rePasswordKV = regexp.MustCompile(`(?i)\b(password|passwd|pwd)\s*=\s*[^ \t\n\r]+`)
	reUserKV     = regexp.MustCompile(`(?i)\b(user|username)\s*=\s*[^ \t\n\r]+`)
	reHostKV     = regexp.MustCompile(`(?i)\bhost\s*=\s*[^ \t\n\r]+`)
	rePortKV     = regexp.MustCompile(`(?i)\bport\s*=\s*[^ \t\n\r]+`)
	reDBNameKV   = regexp.MustCompile(`(?i)\b(dbname|database)\s*=\s*[^ \t\n\r]+`)

	// URL-ish credentials: protocol://user:pass@host:port/...
	reURLCreds = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)([^:/@\s]+):([^@/\s]+)@`)
)

func sanitizePgErrorString(s string) string {
	if s == "" {
		return ""
	}

	out := s
	out = rePasswordKV.ReplaceAllString(out, "$1=[REDACTED]")
	out = reUserKV.ReplaceAllString(out, "$1=[REDACTED]")
	out = reHostKV.ReplaceAllString(out, "host=[REDACTED]")
	out = rePortKV.ReplaceAllString(out, "port=[REDACTED]")
	out = reDBNameKV.ReplaceAllString(out, "$1=[REDACTED]")
	out = reURLCreds.ReplaceAllString(out, "$1[REDACTED]:[REDACTED]@")

	// Common connection string tokens sometimes appear as "tcp://10.0.0.1:5432" etc.
	// We keep it minimal: redact obvious private network ranges.
	out = redactPrivateIPs(out)
	return out
}

func redactPrivateIPs(s string) string {
	// Lightweight token-based redaction; avoids heavy IP parsing.
	tokens := strings.Fields(s)
	for i, t := range tokens {
		tt := strings.Trim(t, "[](),;\"'")
		if strings.HasPrefix(tt, "10.") || strings.HasPrefix(tt, "192.168.") || strings.HasPrefix(tt, "172.16.") || strings.HasPrefix(tt, "172.17.") || strings.HasPrefix(tt, "172.18.") || strings.HasPrefix(tt, "172.19.") || strings.HasPrefix(tt, "172.2") || strings.HasPrefix(tt, "172.30.") || strings.HasPrefix(tt, "172.31.") {
			tokens[i] = strings.ReplaceAll(tokens[i], tt, "[REDACTED_IP]")
		}
	}
	if len(tokens) == 0 {
		return s
	}
	return strings.Join(tokens, " ")
}

