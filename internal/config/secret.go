package config

import (
	"log/slog"
	"runtime/secret"
)

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
// Use Do for better security if possible.
func (s Secret) Expose() string {
	return string(s)
}

// Do executes f with the secret value, ensuring that any temporary storage
// used by f (and the secret value itself if it's no longer used) is erased
// in a timely manner.
func (s Secret) Do(f func(string)) {
	secret.Do(func() {
		f(string(s))
	})
}
