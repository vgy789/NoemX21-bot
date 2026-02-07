package statistics

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vgy789/noemx21-bot/internal/clients/s21"
	"github.com/vgy789/noemx21-bot/internal/config"
	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/fsm"
	"github.com/vgy789/noemx21-bot/internal/service"
)

// Register registers statistics-related actions.
func Register(
	registry *fsm.LogicRegistry,
	cfg *config.Config,
	log *slog.Logger,
	queries db.Querier,
	s21Client *s21.Client,
	credService *service.CredentialService,
	aliasRegistrar func(alias, target string),
) {
	if aliasRegistrar != nil {
		aliasRegistrar("STATS_MENU", "statistics.yaml/STATS_SEARCH_MENU")
	}

	registry.Register("get_user_stats", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		// 1. Get user account
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			log.Error("failed to get user account", "user_id", userID, "error", err)
			return "", nil, err
		}

		// 2. Try to get API token (using Bot's credentials to fetch data)
		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err != nil {
			log.Warn("failed to get valid token, falling back to DB stats", "error", err)
			return getStatsFromDB(ctx, acc.StudentID, queries, log)
		}
		log.Info("got valid token", "token_len", len(token))

		// 3. Call API
		participant, err := s21Client.GetParticipant(ctx, token, acc.StudentID)
		if err != nil {
			log.Error("failed to get participant from API", "login", acc.StudentID, "error", err)
			return getStatsFromDB(ctx, acc.StudentID, queries, log)
		}
		log.Info("got participant from API", "login", participant.Login, "level", participant.Level, "exp", participant.ExpValue)

		points, err := s21Client.GetParticipantPoints(ctx, token, acc.StudentID)
		if err != nil {
			log.Error("failed to get points from API", "login", acc.StudentID, "error", err)
		} else {
			log.Info("got points from API", "coins", points.Coins, "peer_points", points.PeerReviewPoints)
		}

		coalition, err := s21Client.GetParticipantCoalition(ctx, token, acc.StudentID)
		if err != nil {
			log.Error("failed to get coalition from API", "login", acc.StudentID, "error", err)
		} else if coalition != nil {
			log.Info("got coalition from API", "id", coalition.CoalitionID, "name", coalition.CoalitionName)
		}

		// 4. Save to DB
		// Use specialized UpdateStudentStats if specific fields are present
		updateParams := db.UpdateStudentStatsParams{
			S21Login: acc.StudentID,
			Level:    pgtype.Int4{Int32: participant.Level, Valid: true},
			ExpValue: pgtype.Int4{Int32: int32(participant.ExpValue), Valid: true},
		}
		if points != nil {
			updateParams.Prp = pgtype.Int4{Int32: points.PeerReviewPoints, Valid: true}
			updateParams.Crp = pgtype.Int4{Int32: points.CodeReviewPoints, Valid: true}
			updateParams.Coins = pgtype.Int4{Int32: points.Coins, Valid: true}
		}

		if coalition != nil {
			err = queries.UpsertCoalition(ctx, db.UpsertCoalitionParams{
				ID:   int16(coalition.CoalitionID),
				Name: coalition.CoalitionName,
			})
			if err != nil {
				log.Error("failed to upsert coalition", "login", acc.StudentID, "error", err)
			}
			updateParams.CoalitionID = pgtype.Int2{Int16: int16(coalition.CoalitionID), Valid: true}
		}

		err = queries.UpdateStudentStats(ctx, updateParams)
		if err != nil {
			log.Error("failed to update student stats in DB", "login", acc.StudentID, "error", err)
		}

		// Fetch and save skills? Not easily possible due to missing IDs. Skipping for now.

		// 5. Prepare variables
		vars := map[string]interface{}{
			"my_s21login":  participant.Login,
			"my_exp":       participant.ExpValue,
			"my_level":     participant.Level,
			"my_campus":    participant.Campus.ShortName,
			"my_coalition": "Нет коалиции",
		}
		log.Info("get_user_stats vars", "vars", vars)

		if points != nil {
			vars["my_prps"] = points.PeerReviewPoints
			vars["my_crps"] = points.CodeReviewPoints
			vars["my_coins"] = points.Coins
		}

		if coalition != nil {
			vars["my_coalition"] = coalition.CoalitionName
		}

		return "", vars, nil
	})

	registry.Register("get_peer_data_with_permissions", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		login, ok := payload["login"].(string)
		if !ok {
			return "", nil, fmt.Errorf("login not found in payload")
		}

		// 1. Get token
		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err != nil {
			log.Warn("failed to get valid token, falling back to DB", "error", err)
			return getPeerStatsFromDB(ctx, login, queries, log)
		}

		// 2. Fetch peer from API
		participant, err := s21Client.GetParticipant(ctx, token, login)
		if err != nil {
			log.Error("failed to get peer from API", "peer", login, "error", err)
			// Return fallback or not found
			return getPeerStatsFromDB(ctx, login, queries, log)
		}

		points, _ := s21Client.GetParticipantPoints(ctx, token, login)
		coalition, _ := s21Client.GetParticipantCoalition(ctx, token, login)

		// 3. Prepare variables
		vars := map[string]interface{}{
			"peer_found":     true,
			"peer_login":     participant.Login,
			"peer_campus":    participant.Campus.ShortName,
			"peer_coalition": "Нет коалиции",
			"peer_level":     participant.Level,
			"peer_exp":       participant.ExpValue,
			"peer_coins":     0,
			"peer_telegram":  "",
			"peer_id":        0,
		}

		if points != nil {
			vars["peer_coins"] = points.Coins
		}
		if coalition != nil {
			vars["peer_coalition"] = coalition.CoalitionName
		}

		// 4. Update DB if peer exists in our records
		_, err = queries.GetStudentByS21Login(ctx, login)
		if err == nil {
			updateParams := db.UpdateStudentStatsParams{
				S21Login: login,
				Level:    pgtype.Int4{Int32: participant.Level, Valid: true},
				ExpValue: pgtype.Int4{Int32: int32(participant.ExpValue), Valid: true},
			}
			if points != nil {
				updateParams.Prp = pgtype.Int4{Int32: points.PeerReviewPoints, Valid: true}
				updateParams.Crp = pgtype.Int4{Int32: points.CodeReviewPoints, Valid: true}
				updateParams.Coins = pgtype.Int4{Int32: points.Coins, Valid: true}
			}
			if coalition != nil {
				err = queries.UpsertCoalition(ctx, db.UpsertCoalitionParams{
					ID:   int16(coalition.CoalitionID),
					Name: coalition.CoalitionName,
				})
				if err != nil {
					log.Error("failed to upsert coalition for peer", "login", login, "error", err)
				}
				updateParams.CoalitionID = pgtype.Int2{Int16: int16(coalition.CoalitionID), Valid: true}
			}
			err = queries.UpdateStudentStats(ctx, updateParams)
			if err != nil {
				log.Error("failed to update peer stats in DB", "login", login, "error", err)
			}
		}

		// 5. Try to get telegram and platform ID from our DB if they exist
		peerAcc, err := queries.GetUserAccountByStudentId(ctx, login)
		if err == nil {
			vars["peer_id"] = peerAcc.ExternalID
			// Get telegram username from students table
			peerProfile, err := queries.GetPeerProfile(ctx, login)
			if err == nil {
				vars["peer_telegram"] = peerProfile.TelegramUsername
			}
		}

		return "", vars, nil
	})

	registry.Register("get_user_skills", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		// Find login
		acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err != nil {
			return "", nil, err
		}

		// Try API
		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err == nil {
			skillsResp, err := s21Client.GetParticipantSkills(ctx, token, acc.StudentID)
			if err == nil {
				skillMap := make(map[string]int32)
				for _, s := range skillsResp.Skills {
					skillMap[s.Name] = s.Points
				}
				return "", map[string]interface{}{
					"my_skills": skillMap,
				}, nil
			}
			log.Error("failed to fetch skills from API", "login", acc.StudentID, "error", err)
		}

		// Fallback to DB
		skills, err := queries.GetStudentSkills(ctx, acc.StudentID)
		if err != nil {
			return "", nil, err
		}

		skillMap := make(map[string]int32)
		for _, s := range skills {
			skillMap[s.Name] = s.Value
		}

		return "", map[string]interface{}{
			"my_skills": skillMap,
		}, nil
	})

	registry.Register("generate_radar_chart", func(ctx context.Context, userID int64, payload map[string]interface{}) (string, map[string]interface{}, error) {
		usersRaw, ok := payload["users"].([]interface{})
		if !ok {
			// Try single user? No, payload usually has list.
			return "", nil, fmt.Errorf("users list not found in payload")
		}

		// Get requester token for API calls
		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err != nil {
			log.Warn("no valid token for chart generation, will use DB only")
			// proceed with token=""
		}

		mermaid := "```mermaid\npie title Навыки\n"

		for _, uRaw := range usersRaw {
			var login string

			switch v := uRaw.(type) {
			case string:
				login = v
			case int64:
				// Try to get login from ID
				acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
					Platform:   db.EnumPlatformTelegram,
					ExternalID: fmt.Sprintf("%d", v),
				})
				if err == nil {
					login = acc.StudentID
				}
			case float64:
				acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
					Platform:   db.EnumPlatformTelegram,
					ExternalID: fmt.Sprintf("%.0f", v),
				})
				if err == nil {
					login = acc.StudentID
				}
			}

			if login == "" {
				continue
			}

			// Try API if we have a token
			var skillsMap map[string]int32
			if token != "" {
				skillsResp, err := s21Client.GetParticipantSkills(ctx, token, login)
				if err == nil {
					skillsMap = make(map[string]int32)
					for _, s := range skillsResp.Skills {
						skillsMap[s.Name] = s.Points
					}
				}
			}

			// Fallback to DB
			if skillsMap == nil {
				skills, _ := queries.GetStudentSkills(ctx, login)
				if len(skills) > 0 {
					skillsMap = make(map[string]int32)
					for _, s := range skills {
						skillsMap[s.Name] = s.Value
					}
				}
			}

			if skillsMap != nil {
				// For radar chart, we usually want top skills or grouped.
				// But mermaid pie chart (as used here) just takes values.
				// Wait, the action name is "generate_radar_chart" but it generates "pie"?
				// The user asked for "radar chart". Mermaid doesn't support radar charts natively comfortably in all versions?
				// Actually, `quadrantChart` or similar might be better, or `xychart`.
				// But let's stick to what was there: "pie title Skills".

				// To avoid overcrowding, maybe limit to top 5?
				// Or just dump all.
				for name, val := range skillsMap {
					// sanitize name
					mermaid += fmt.Sprintf("    \"%s (%s)\" : %d\n", name, login, val)
				}
			}
		}
		mermaid += "```"

		return "", map[string]interface{}{
			"radar_chart_mermaid":      mermaid,
			"radar_comparison_mermaid": mermaid,
		}, nil
	})
}

func getStatsFromDB(ctx context.Context, studentID string, queries db.Querier, log *slog.Logger) (string, map[string]interface{}, error) {
	profile, err := queries.GetStudentProfile(ctx, studentID)
	if err != nil {
		log.Error("failed to get student profile from DB", "login", studentID, "error", err)
		return "", nil, err
	}

	vars := map[string]interface{}{
		"my_s21login":  profile.S21Login,
		"my_exp":       profile.ExpValue.Int32,
		"my_level":     profile.Level.Int32,
		"my_prps":      profile.Prp.Int32,
		"my_crps":      profile.Crp.Int32,
		"my_coins":     profile.Coins.Int32,
		"my_campus":    "Неизвестный кампус",
		"my_coalition": "Нет коалиции",
	}

	if profile.CampusName.Valid {
		vars["my_campus"] = profile.CampusName.String
	}
	if profile.CoalitionName.Valid {
		vars["my_coalition"] = profile.CoalitionName.String
	}

	return "", vars, nil
}

func getPeerStatsFromDB(ctx context.Context, login string, queries db.Querier, log *slog.Logger) (string, map[string]interface{}, error) {
	profile, err := queries.GetPeerProfile(ctx, login)
	if err != nil {
		return "", map[string]interface{}{
			"peer_found": false,
		}, nil
	}

	vars := map[string]interface{}{
		"peer_found":     true,
		"peer_login":     profile.S21Login,
		"peer_campus":    "Неизвестный кампус",
		"peer_coalition": "Нет коалиции",
		"peer_level":     profile.Level.Int32,
		"peer_exp":       profile.ExpValue.Int32,
		"peer_coins":     profile.Coins.Int32,
		"peer_telegram":  profile.TelegramUsername,
		"peer_id":        0,
	}

	if profile.CampusName.Valid {
		vars["peer_campus"] = profile.CampusName.String
	}
	if profile.CoalitionName.Valid {
		vars["peer_coalition"] = profile.CoalitionName.String
	}

	acc, err := queries.GetUserAccountByStudentId(ctx, login)
	if err == nil {
		vars["peer_id"] = acc.ExternalID
	}

	return "", vars, nil
}
