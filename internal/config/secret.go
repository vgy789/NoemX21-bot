package config

import "log/slog"

// Secret is a type for data that should be hidden in logs.
type Secret string

// String returns [REDACTED] instead of secret value.
func (s Secret) String() string {
	return "[REDACTED]"
}

// LogValue returns [REDACTED] instead of secret value.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue("[REDACTED]")
}

// Expose returns secret value as string.
func (s Secret) Expose() string {
	return string(s)
}
