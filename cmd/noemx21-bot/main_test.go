package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetupLogger(t *testing.T) {
	tests := []struct {
		name       string
		production bool
		logLevel   string
	}{
		{"debug non-production", false, "debug"},
		{"info production", true, "info"},
		{"warn level", false, "warn"},
		{"error level", true, "error"},
		{"default fallback", false, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := setupLogger(tt.production, tt.logLevel)
			assert.NotNil(t, log)
		})
	}
}
