package actions

import (
	"log/slog"

	"github.com/vgy789/noemx21-bot/internal/clients/rocketchat"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// Dependencies holds all services needed by plugins.
type Dependencies struct {
	Config         *config.Config
	Log            *slog.Logger
	StudentSvc     service.StudentService
	Queries        db.Querier
	RCClient       *rocketchat.Client
	S21Client      *s21.Client
	AliasRegistrar func(alias, target string)
}

// Plugin defines the interface for an action plugin.
type Plugin interface {
	ID() string
	Register(registry *fsm.LogicRegistry, deps *Dependencies)
}

var (
	plugins []Plugin
)

// Register adds a plugin to the global registry.
func Register(p Plugin) {
	plugins = append(plugins, p)
}

// GetPlugins returns all registered plugins.
func GetPlugins() []Plugin {
	return plugins
}

// BasicPlugin is a helper to wrap a registration function.
type BasicPlugin struct {
	id    string
	regFn func(registry *fsm.LogicRegistry, deps *Dependencies)
}

func (p *BasicPlugin) ID() string { return p.id }
func (p *BasicPlugin) Register(registry *fsm.LogicRegistry, deps *Dependencies) {
	p.regFn(registry, deps)
}

func NewBasicPlugin(id string, regFn func(registry *fsm.LogicRegistry, deps *Dependencies)) Plugin {
	return &BasicPlugin{id: id, regFn: regFn}
}
