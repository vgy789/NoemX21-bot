package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vgy789/noemx21-bot/internal/initialization"
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
			log := initialization.SetupLogger(tt.production, tt.logLevel)
			assert.NotNil(t, log)
		})
	}
}
