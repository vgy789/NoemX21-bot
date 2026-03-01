package fsm

import (
	"strconv"
	"strings"
)

type PRRNotifyAction string

const (
	PRRNotifyActionResume PRRNotifyAction = "resume"
	PRRNotifyActionClose  PRRNotifyAction = "close"
	PRRNotifyActionMenu   PRRNotifyAction = "menu"

	prrNotifyCallbackResumePrefix = "prrnr:"
	prrNotifyCallbackClosePrefix  = "prrnc:"
	prrNotifyCallbackMenu         = "prrnm"
)

func BuildPRRNotifyResumeCallback(prrID int64) string {
	return prrNotifyCallbackResumePrefix + strconv.FormatInt(prrID, 10)
}

func BuildPRRNotifyCloseCallback(prrID int64) string {
	return prrNotifyCallbackClosePrefix + strconv.FormatInt(prrID, 10)
}

func BuildPRRNotifyMenuCallback() string {
	return prrNotifyCallbackMenu
}

func ParsePRRNotifyCallback(data string) (PRRNotifyAction, int64, bool) {
	trimmed := strings.TrimSpace(data)
	if trimmed == prrNotifyCallbackMenu {
		return PRRNotifyActionMenu, 0, true
	}
	if after, ok := strings.CutPrefix(trimmed, prrNotifyCallbackResumePrefix); ok {
		id, err := strconv.ParseInt(after, 10, 64)
		if err != nil || id <= 0 {
			return "", 0, false
		}
		return PRRNotifyActionResume, id, true
	}
	if after, ok := strings.CutPrefix(trimmed, prrNotifyCallbackClosePrefix); ok {
		id, err := strconv.ParseInt(after, 10, 64)
		if err != nil || id <= 0 {
			return "", 0, false
		}
		return PRRNotifyActionClose, id, true
	}
	return "", 0, false
}
