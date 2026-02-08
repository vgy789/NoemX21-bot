package statistics

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"

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
			S21Login:     acc.StudentID,
			Level:        pgtype.Int4{Int32: participant.Level, Valid: true},
			ExpValue:     pgtype.Int4{Int32: int32(participant.ExpValue), Valid: true},
			Status:       db.NullEnumStudentStatus{EnumStudentStatus: db.EnumStudentStatus(participant.Status), Valid: true},
			ParallelName: pgtype.Text{String: "", Valid: false},
			ClassName:    pgtype.Text{String: "", Valid: false},
		}
		if participant.ParallelName != nil {
			updateParams.ParallelName = pgtype.Text{String: *participant.ParallelName, Valid: true}
		}
		if participant.ClassName != nil {
			updateParams.ClassName = pgtype.Text{String: *participant.ClassName, Valid: true}
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

		feedback, err := s21Client.GetParticipantFeedback(ctx, token, acc.StudentID)
		if err != nil {
			log.Error("failed to get feedback from API", "login", acc.StudentID, "error", err)
		}

		// 5. Prepare variables
		vars := map[string]interface{}{
			"my_s21login":     participant.Login,
			"my_exp":          participant.ExpValue,
			"my_level":        participant.Level,
			"my_campus":       participant.Campus.ShortName,
			"my_status":       participant.Status,
			"my_coalition":    "Нет коалиции",
			"my_interest":     "0.00",
			"my_friendliness": "0.00",
			"my_punctuality":  "0.00",
			"my_thoroughness": "0.00",
			"peer_status":     "—", // Placeholder as it's used in the template
		}

		if participant.ClassName != nil {
			vars["my_class"] = *participant.ClassName
		} else {
			vars["my_class"] = "—"
		}
		if participant.ParallelName != nil {
			vars["my_parallel"] = *participant.ParallelName
		} else {
			vars["my_parallel"] = "—"
		}

		if feedback != nil {
			log.Info("got feedback from API", "interest", feedback.Integrity, "friendliness", feedback.Friendliness, "punctuality", feedback.Punctuality, "thoroughness", feedback.Thoroughness)
			vars["my_interest"] = fmt.Sprintf("%.2f", feedback.Integrity)
			vars["my_friendliness"] = fmt.Sprintf("%.2f", feedback.Friendliness)
			vars["my_punctuality"] = fmt.Sprintf("%.2f", feedback.Punctuality)
			vars["my_thoroughness"] = fmt.Sprintf("%.2f", feedback.Thoroughness)
		} else {
			log.Warn("feedback from API is nil", "login", acc.StudentID)
		}

		log.Info("get_user_stats vars final", "vars", vars)

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
		feedback, _ := s21Client.GetParticipantFeedback(ctx, token, login)

		// 3. Prepare variables
		vars := map[string]interface{}{
			"peer_found":        true,
			"peer_login":        participant.Login,
			"peer_campus":       participant.Campus.ShortName,
			"peer_coalition":    "Нет коалиции",
			"peer_level":        participant.Level,
			"peer_exp":          participant.ExpValue,
			"peer_status":       participant.Status,
			"peer_coins":        0,
			"peer_telegram":     "",
			"peer_id":           0,
			"peer_interest":     "0.00",
			"peer_friendliness": "0.00",
			"peer_punctuality":  "0.00",
			"peer_thoroughness": "0.00",
			"peer_prps":         0,
			"peer_crps":         0,
			"my_status":         "—",
			"my_class":          "—",
			"my_parallel":       "—",
			"my_campus":         "—",
			"my_coalition":      "—",
			"my_level":          0,
			"my_exp":            0,
			"my_interest":       "0.00",
			"my_friendliness":   "0.00",
			"my_punctuality":    "0.00",
			"my_thoroughness":   "0.00",
			"my_prps":           0,
			"my_crps":           0,
		}

		// Fill current user info for comparison
		userAcc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
			Platform:   db.EnumPlatformTelegram,
			ExternalID: fmt.Sprintf("%d", userID),
		})
		if err == nil {
			// Try API for current user to get fresh stats and social metrics
			myParticipant, pErr := s21Client.GetParticipant(ctx, token, userAcc.StudentID)
			myPoints, _ := s21Client.GetParticipantPoints(ctx, token, userAcc.StudentID)
			myCoalition, _ := s21Client.GetParticipantCoalition(ctx, token, userAcc.StudentID)
			myFeedback, _ := s21Client.GetParticipantFeedback(ctx, token, userAcc.StudentID)

			if pErr == nil {
				vars["my_status"] = myParticipant.Status
				vars["my_level"] = myParticipant.Level
				vars["my_exp"] = myParticipant.ExpValue
				vars["my_campus"] = myParticipant.Campus.ShortName
				if myParticipant.ClassName != nil {
					vars["my_class"] = *myParticipant.ClassName
				}
				if myParticipant.ParallelName != nil {
					vars["my_parallel"] = *myParticipant.ParallelName
				}
			} else {
				// Fallback to DB
				userProfile, err := queries.GetStudentProfile(ctx, userAcc.StudentID)
				if err == nil {
					vars["my_status"] = string(userProfile.Status.EnumStudentStatus)
					vars["my_level"] = userProfile.Level.Int32
					vars["my_exp"] = userProfile.ExpValue.Int32
					vars["my_prps"] = userProfile.Prp.Int32
					vars["my_crps"] = userProfile.Crp.Int32
					if userProfile.ClassName.Valid {
						vars["my_class"] = userProfile.ClassName.String
					}
					if userProfile.ParallelName.Valid {
						vars["my_parallel"] = userProfile.ParallelName.String
					}
					if userProfile.CampusName.Valid {
						vars["my_campus"] = userProfile.CampusName.String
					}
					if userProfile.CoalitionName.Valid {
						vars["my_coalition"] = userProfile.CoalitionName.String
					}
				}
			}

			if myPoints != nil {
				vars["my_prps"] = myPoints.PeerReviewPoints
				vars["my_crps"] = myPoints.CodeReviewPoints
				vars["my_coins"] = myPoints.Coins
			}
			if myCoalition != nil {
				vars["my_coalition"] = myCoalition.CoalitionName
			}
			if myFeedback != nil {
				vars["my_interest"] = fmt.Sprintf("%.2f", myFeedback.Integrity)
				vars["my_friendliness"] = fmt.Sprintf("%.2f", myFeedback.Friendliness)
				vars["my_punctuality"] = fmt.Sprintf("%.2f", myFeedback.Punctuality)
				vars["my_thoroughness"] = fmt.Sprintf("%.2f", myFeedback.Thoroughness)
			}
		}

		if participant.ClassName != nil {
			vars["peer_class"] = *participant.ClassName
		} else {
			vars["peer_class"] = "—"
		}
		if participant.ParallelName != nil {
			vars["peer_parallel"] = *participant.ParallelName
		} else {
			vars["peer_parallel"] = "—"
		}

		if points != nil {
			vars["peer_coins"] = points.Coins
			vars["peer_prps"] = points.PeerReviewPoints
			vars["peer_crps"] = points.CodeReviewPoints
		}
		if coalition != nil {
			vars["peer_coalition"] = coalition.CoalitionName
		}
		if feedback != nil {
			log.Info("got peer feedback from API", "login", login, "interest", feedback.Integrity, "friendliness", feedback.Friendliness, "punctuality", feedback.Punctuality, "thoroughness", feedback.Thoroughness)
			vars["peer_interest"] = fmt.Sprintf("%.2f", feedback.Integrity)
			vars["peer_friendliness"] = fmt.Sprintf("%.2f", feedback.Friendliness)
			vars["peer_punctuality"] = fmt.Sprintf("%.2f", feedback.Punctuality)
			vars["peer_thoroughness"] = fmt.Sprintf("%.2f", feedback.Thoroughness)
		} else {
			log.Warn("peer feedback from API is nil", "login", login)
		}

		log.Info("get_peer_data_with_permissions vars final", "vars", vars)
		_, err = queries.GetStudentByS21Login(ctx, login)
		if err == nil {
			updateParams := db.UpdateStudentStatsParams{
				S21Login:     login,
				Level:        pgtype.Int4{Int32: participant.Level, Valid: true},
				ExpValue:     pgtype.Int4{Int32: int32(participant.ExpValue), Valid: true},
				Status:       db.NullEnumStudentStatus{EnumStudentStatus: db.EnumStudentStatus(participant.Status), Valid: true},
				ParallelName: pgtype.Text{String: "", Valid: false},
				ClassName:    pgtype.Text{String: "", Valid: false},
			}
			if participant.ParallelName != nil {
				updateParams.ParallelName = pgtype.Text{String: *participant.ParallelName, Valid: true}
			}
			if participant.ClassName != nil {
				updateParams.ClassName = pgtype.Text{String: *participant.ClassName, Valid: true}
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
					// Save to DB
					hash := fnv.New32a()
					hash.Write([]byte(s.Name))
					skillID := int32(hash.Sum32())

					skill, err := queries.UpsertSkill(ctx, db.UpsertSkillParams{
						ID:       skillID,
						Name:     s.Name,
						Category: pgtype.Text{String: "General", Valid: true},
					})
					if err == nil {
						_ = queries.UpsertStudentSkill(ctx, db.UpsertStudentSkillParams{
							StudentID: acc.StudentID,
							SkillID:   skill.ID,
							Value:     s.Points,
						})
					}
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
			return "", nil, fmt.Errorf("users list not found in payload")
		}

		// Get requester token for API calls
		token, err := credService.GetValidToken(ctx, cfg.Init.SchoolLogin)
		if err != nil {
			log.Warn("no valid token for chart generation, will use DB only")
		}

		usersData := make(map[string]map[string]int32)

		for _, uRaw := range usersRaw {
			var login string

			log.Debug("processing user identifier", "raw", uRaw)

			switch v := uRaw.(type) {
			case string:
				if v == "$context.user_id" {
					// Handle unparsed variable from FSM
					acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
						Platform:   db.EnumPlatformTelegram,
						ExternalID: fmt.Sprintf("%d", userID),
					})
					if err == nil {
						login = acc.StudentID
					}
				} else if v == "$context.peer_login" {
					// Try to get from context if possible (though system actions don't have easy access to full context here,
					// we can rely on the fact that peer_login should be passed as a resolved string if it worked)
					log.Warn("peer_login reached action as unresolved placeholder", "v", v)
					// We don't have the context here, so if it's not resolved by engine, we can't do much unless we pass it specifically.
				} else if _, err := strconv.ParseInt(v, 10, 64); err == nil {
					// If it's a numeric string, it's likely a Telegram user ID
					acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
						Platform:   db.EnumPlatformTelegram,
						ExternalID: v,
					})
					if err == nil {
						login = acc.StudentID
					} else {
						// Not found in DB, assume it might be a login that just happens to be numeric
						login = v
					}
				} else {
					login = v
				}
			case int64:
				// Try to get login from ID
				acc, err := queries.GetUserAccountByExternalId(ctx, db.GetUserAccountByExternalIdParams{
					Platform:   db.EnumPlatformTelegram,
					ExternalID: strconv.FormatInt(v, 10),
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

			if login == "" || strings.HasPrefix(login, "$context.") {
				log.Warn("login is empty or unresolved in generate_radar_chart", "uRaw", uRaw, "resolved", login)
				continue
			}

			log.Info("collecting skills for radar chart", "login", login)

			// Try API if we have a token
			var skillsMap map[string]int32
			if token != "" {
				skillsResp, err := s21Client.GetParticipantSkills(ctx, token, login)
				if err == nil {
					skillsMap = make(map[string]int32)
					for _, s := range skillsResp.Skills {
						skillsMap[s.Name] = s.Points
						// Save to DB
						hash := fnv.New32a()
						hash.Write([]byte(s.Name))
						skillID := int32(hash.Sum32())

						skill, _ := queries.UpsertSkill(ctx, db.UpsertSkillParams{
							ID:       skillID,
							Name:     s.Name,
							Category: pgtype.Text{String: "General", Valid: true},
						})
						_ = queries.UpsertStudentSkill(ctx, db.UpsertStudentSkillParams{
							StudentID: login,
							SkillID:   skill.ID,
							Value:     s.Points,
						})
					}
					log.Info("got skills from API", "login", login, "count", len(skillsMap))
				} else {
					log.Error("failed to get skills from API", "login", login, "error", err)
				}
			}

			// Fallback to DB
			if skillsMap == nil {
				skills, err := queries.GetStudentSkills(ctx, login)
				if err == nil && len(skills) > 0 {
					skillsMap = make(map[string]int32)
					for _, s := range skills {
						skillsMap[s.Name] = s.Value
					}
					log.Info("got skills from DB", "login", login, "count", len(skillsMap))
				} else {
					log.Warn("no skills found in DB", "login", login, "error", err)
				}
			}

			if skillsMap != nil {
				usersData[login] = skillsMap
			}
		}

		if len(usersData) == 0 {
			log.Warn("no data found for any user in generate_radar_chart")
			return "", nil, nil
		}

		chartPath, err := generateRadarChart(usersData)
		if err != nil {
			log.Error("failed to generate radar chart image", "error", err)
			return "", nil, err
		}

		return "", map[string]interface{}{
			"radar_chart_path":      chartPath,
			"radar_comparison_path": chartPath,
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
		"my_s21login":     profile.S21Login,
		"my_exp":          profile.ExpValue.Int32,
		"my_level":        profile.Level.Int32,
		"my_prps":         profile.Prp.Int32,
		"my_crps":         profile.Crp.Int32,
		"my_coins":        profile.Coins.Int32,
		"my_campus":       "Неизвестный кампус",
		"my_coalition":    "Нет коалиции",
		"my_status":       string(profile.Status.EnumStudentStatus),
		"my_class":        "—",
		"my_parallel":     "—",
		"my_interest":     "0.00",
		"my_friendliness": "0.00",
		"my_punctuality":  "0.00",
		"my_thoroughness": "0.00",
		"peer_status":     "—",
	}

	if profile.CampusName.Valid {
		vars["my_campus"] = profile.CampusName.String
	}
	if profile.CoalitionName.Valid {
		vars["my_coalition"] = profile.CoalitionName.String
	}
	if profile.ClassName.Valid {
		vars["my_class"] = profile.ClassName.String
	}
	if profile.ParallelName.Valid {
		vars["my_parallel"] = profile.ParallelName.String
	}

	return "", vars, nil
}

func getPeerStatsFromDB(ctx context.Context, login string, queries db.Querier, log *slog.Logger) (string, map[string]interface{}, error) {
	profile, err := queries.GetPeerProfile(ctx, login)
	if err != nil {
		log.Debug("peer not found in DB", "login", login, "error", err)
		return "", map[string]interface{}{
			"peer_found": false,
		}, nil
	}

	vars := map[string]interface{}{
		"peer_found":        true,
		"peer_login":        profile.S21Login,
		"peer_campus":       "Неизвестный кампус",
		"peer_coalition":    "Нет коалиции",
		"peer_level":        profile.Level.Int32,
		"peer_exp":          profile.ExpValue.Int32,
		"peer_coins":        profile.Coins.Int32,
		"peer_telegram":     profile.TelegramUsername,
		"peer_status":       string(profile.Status.EnumStudentStatus),
		"peer_class":        "—",
		"peer_parallel":     "—",
		"peer_id":           0,
		"peer_interest":     "0.00",
		"peer_friendliness": "0.00",
		"peer_punctuality":  "0.00",
		"peer_thoroughness": "0.00",
		"my_status":         "—",
	}

	if profile.CampusName.Valid {
		vars["peer_campus"] = profile.CampusName.String
	}
	if profile.CoalitionName.Valid {
		vars["peer_coalition"] = profile.CoalitionName.String
	}
	if profile.ClassName.Valid {
		vars["peer_class"] = profile.ClassName.String
	}
	if profile.ParallelName.Valid {
		vars["peer_parallel"] = profile.ParallelName.String
	}

	acc, err := queries.GetUserAccountByStudentId(ctx, login)
	if err == nil {
		vars["peer_id"] = acc.ExternalID
	}

	return "", vars, nil
}
