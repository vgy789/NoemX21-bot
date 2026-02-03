package fsm

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// FlowParser handles loading and parsing of flow specifications.
type FlowParser struct {
	baseDir string
	cache   map[string]*FlowSpec
	mu      sync.RWMutex
	log     *slog.Logger
}

// NewFlowParser creates a new parser.
func NewFlowParser(baseDir string, log *slog.Logger) *FlowParser {
	return &FlowParser{
		baseDir: baseDir,
		cache:   make(map[string]*FlowSpec),
		log:     log,
	}
}

// GetFlow loads a flow by name (filename).
func (p *FlowParser) GetFlow(filename string) (*FlowSpec, error) {
	p.mu.RLock()
	if spec, ok := p.cache[filename]; ok {
		p.mu.RUnlock()
		return spec, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check
	if spec, ok := p.cache[filename]; ok {
		return spec, nil
	}

	path := filepath.Join(p.baseDir, filename)
	p.log.Debug("reading flow file", "path", path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read flow file %s: %w", path, err)
	}

	var spec FlowSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse yaml %s: %w", filename, err)
	}

	p.cache[filename] = &spec
	p.log.Debug("flow loaded", "filename", filename, "states", len(spec.States))
	return &spec, nil
}
