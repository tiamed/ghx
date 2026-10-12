// Package redact strips GitHub credentials from logs and error text.
package redact

import "regexp"
import "strings"

var (
	tokenRE  = regexp.MustCompile(`(?i)\b(?:ghp_|gho_|ghu_|ghs_|ghr_|github_pat_)[A-Za-z0-9_]+`)
	bearerRE = regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|token)\s+)\S+`)
	queryRE  = regexp.MustCompile(`(?i)([?&](?:access_token|token|client_secret|password)=)[^&\s]+`)
	userRE   = regexp.MustCompile(`(?i)(https?://)[^/\s:]+:[^/\s@]+@`)
)

// Clean removes known credential material from s.
// token is the raw credential currently in use, even when it has no standard prefix.
func Clean(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "[REDACTED]")
	}
	s = userRE.ReplaceAllString(s, "${1}")
	s = bearerRE.ReplaceAllString(s, "${1}[REDACTED]")
	s = queryRE.ReplaceAllString(s, "${1}[REDACTED]")
	s = tokenRE.ReplaceAllString(s, "[REDACTED]")
	return s
}
