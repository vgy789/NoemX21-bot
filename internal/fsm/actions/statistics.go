package actions

import (
	"github.com/vgy789/noemx21-bot/internal/fsm"
)

func init() {
	Register(NewBasicPlugin("statistics", func(registry *fsm.LogicRegistry, deps *Dependencies) {
		if deps.AliasRegistrar != nil {
			deps.AliasRegistrar("STATS_MENU", "statistics.yaml/STATS_SEARCH_MENU")
		}
		// Future: Register get_user_stats, generate_radar_chart, etc.
	}))
}
