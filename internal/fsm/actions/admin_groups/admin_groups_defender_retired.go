package admin_groups

import (
	"context"

	"github.com/vgy789/noemx21-bot/internal/fsm"
)

var retiredDefenderActionNames = []string{
	"load_group_defender_context",
	"set_group_defender_enabled",
	"set_group_defender_remove_blocked",
	"set_group_defender_recheck_known_members",
	"load_group_defender_campus_filter_options",
	"defender_campus_prev_page",
	"defender_campus_next_page",
	"set_group_defender_filter_campus",
	"load_group_defender_tribe_filter_options",
	"defender_tribe_prev_page",
	"defender_tribe_next_page",
	"set_group_defender_filter_tribe",
	"set_defender_cleanup_scope_unregistered",
	"set_defender_cleanup_scope_blocked",
	"set_defender_cleanup_scope_campus",
	"set_defender_cleanup_scope_tribe",
	"load_group_defender_cleanup_campus_options",
	"set_group_defender_cleanup_campus_target",
	"defender_cleanup_campus_prev_page",
	"defender_cleanup_campus_next_page",
	"load_group_defender_cleanup_tribe_options",
	"set_group_defender_cleanup_tribe_target",
	"defender_cleanup_tribe_prev_page",
	"defender_cleanup_tribe_next_page",
	"run_group_defender",
	"preview_group_defender_violations",
	"add_group_defender_preview_to_whitelist",
	"add_group_defender_whitelist_from_input",
	"remove_group_defender_whitelist",
	"load_group_defender_logs",
}

func registerRetiredDefenderActions(registry *fsm.LogicRegistry) {
	if registry == nil {
		return
	}
	for _, name := range retiredDefenderActionNames {
		registry.Register(name, func(context.Context, int64, map[string]any) (string, map[string]any, error) {
			return "", map[string]any{
				"can_manage_selected_group": false,
				"_alert":                    "Defender отключён: Group Manager переведён в legacy-режим без автоматических удалений.",
			}, nil
		})
	}
}
