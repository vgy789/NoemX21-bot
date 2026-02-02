package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetupLogger(t *testing.T) {
	tests := []struct {
		name string
		env  string
	}{
		{"local", EnvLocal},
		{"prod", EnvProd},
		{"default", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := setupLogger(tt.env)
			assert.NotNil(t, log)
		})
	}
}
