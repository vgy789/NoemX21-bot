package fsm

import (
	"context"
	"errors"
)

var (
	ErrEngineBusy = errors.New("engine is busy processing another request")
)

// FlowSpec represents the structure of a flow YAML file.
type FlowSpec struct {
	InitialState string           `yaml:"initial_state"`
	States       map[string]State `yaml:"states"`
}

type State struct {
	Type        string       `yaml:"type"` // interactive, system, input, final
	Description string       `yaml:"description"`
	Interface   Interface    `yaml:"interface"`
	Transitions []Transition `yaml:"transitions"`
	Logic       Logic        `yaml:"logic"`
	Validation  Validation   `yaml:"validation"`
	OnEnter     []Logic      `yaml:"on_enter"`
	OnExit      []Logic      `yaml:"on_exit"`
}

type Validation struct {
	Regex string `yaml:"regex"`
}

type Interface struct {
	Image        string            `yaml:"image"`         // Optional: Path to image
	Text         map[string]string `yaml:"text"`          // Locale -> Text
	ErrorInvalid map[string]string `yaml:"error_invalid"` // Optional: Locale -> Error Text
	Buttons      []Button          `yaml:"buttons"`
}

type Button struct {
	ID        string `yaml:"id"`
	Label     any    `yaml:"label"`      // String or Map[string]string
	NextState string `yaml:"next_state"` // can be "STATE" or "file.yaml/STATE"
	URL       string `yaml:"url"`        // Optional: URL for link button (mutually exclusive with next_state)
	Row       int    `yaml:"row"`        // Optional: buttons with same Row ID will be in the same row
	Condition string `yaml:"condition"`  // Optional: condition to show the button
	Action    string `yaml:"action"`
}

type Transition struct {
	Condition string `yaml:"condition"`
	NextState string `yaml:"next_state"`
	Trigger   string `yaml:"trigger"`
	Action    string `yaml:"action"`
}

type Logic struct {
	Action  string         `yaml:"action"`
	Payload map[string]any `yaml:"payload"`
}

// UserState represents the current state of a user.
type UserState struct {
	UserID       int64          `json:"user_id"`
	CurrentFlow  string         `json:"current_flow"`  // e.g. "registration.yaml"
	CurrentState string         `json:"current_state"` // e.g. "AWAITING_OTP"
	Context      map[string]any `json:"context"`       // Store arbitrary data
	Language     string         `json:"language"`      // "ru" or "en"
}

// ContextKey is a type for context keys
type ContextKey string

const (
	// ContextKeyS21Login is used to store S21 login in context
	ContextKeyS21Login ContextKey = "s21_login"
	// ContextKeyUserInfo is used to store transport-level user info (e.g. from Telegram)
	ContextKeyUserInfo ContextKey = "user_info"
	// ContextKeyNotifier is used to store transport notifier implementation.
	ContextKeyNotifier ContextKey = "notifier"
	// ContextKeyMemberTagRunner is used to store transport-level member tags runner.
	ContextKeyMemberTagRunner ContextKey = "member_tag_runner"
	// ContextKeyDefenderRunner is used to store transport-level defender runner.
	ContextKeyDefenderRunner ContextKey = "defender_runner"
)

// UserInfo represents basic user metadata from the transport layer
type UserInfo struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
	Platform  string
}

// Notifier sends out-of-band notifications to users on a specific transport.
type Notifier interface {
	NotifyUser(ctx context.Context, userID int64, text string) error
}

// RenderNotifier sends out-of-band notifications with buttons.
type RenderNotifier interface {
	NotifyUserRender(ctx context.Context, userID int64, render *RenderObject) error
}

// MemberTagRunMode defines manual member tags run behavior.
type MemberTagRunMode string

const (
	MemberTagRunModeKeepExisting  MemberTagRunMode = "keep_existing"
	MemberTagRunModeClearAndApply MemberTagRunMode = "clear_then_apply"
)

// MemberTagRunResult contains aggregated counters for a manual run.
type MemberTagRunResult struct {
	Updated             int
	SkippedExisting     int
	SkippedUnregistered int
	SkippedNotMember    int
	SkippedNoRights     int
	Errors              int
}

// MemberTagRunner executes member-tags operations bound to transport capabilities.
type MemberTagRunner interface {
	RunGroupMemberTags(ctx context.Context, ownerTelegramUserID, chatID int64, mode MemberTagRunMode) (MemberTagRunResult, error)
	SyncMemberTagsForRegisteredUser(ctx context.Context, telegramUserID int64) error
}

// DefenderRunResult contains aggregated counters for a defender run.
type DefenderRunResult struct {
	Removed             int
	SkippedWhitelist    int
	SkippedNotMember    int
	SkippedNoRights     int
	SkippedUnregistered int
	SkippedBlocked      int
	Errors              int
}

// DefenderPreviewItem describes a user that matches defender rules during preview.
type DefenderPreviewItem struct {
	TelegramUserID int64
	DisplayName    string
	Username       string
	Reason         string
}

// DefenderRunner executes defender operations bound to transport capabilities.
type DefenderRunner interface {
	RunGroupDefender(ctx context.Context, ownerTelegramUserID, chatID int64) (DefenderRunResult, error)
	PreviewGroupDefenderCandidates(ctx context.Context, ownerTelegramUserID, chatID int64) ([]DefenderPreviewItem, error)
	ResolveGroupMemberIdentity(ctx context.Context, ownerTelegramUserID, chatID, telegramUserID int64) (displayName, username string, err error)
}

// NotifierFromContext extracts a Notifier from context.
func NotifierFromContext(ctx context.Context) (Notifier, bool) {
	if ctx == nil {
		return nil, false
	}
	n, ok := ctx.Value(ContextKeyNotifier).(Notifier)
	return n, ok && n != nil
}

// RenderNotifierFromContext extracts a RenderNotifier from context.
func RenderNotifierFromContext(ctx context.Context) (RenderNotifier, bool) {
	if ctx == nil {
		return nil, false
	}
	n, ok := ctx.Value(ContextKeyNotifier).(RenderNotifier)
	return n, ok && n != nil
}

// MemberTagRunnerFromContext extracts a MemberTagRunner from context.
func MemberTagRunnerFromContext(ctx context.Context) (MemberTagRunner, bool) {
	if ctx == nil {
		return nil, false
	}
	r, ok := ctx.Value(ContextKeyMemberTagRunner).(MemberTagRunner)
	return r, ok && r != nil
}

// DefenderRunnerFromContext extracts a DefenderRunner from context.
func DefenderRunnerFromContext(ctx context.Context) (DefenderRunner, bool) {
	if ctx == nil {
		return nil, false
	}
	r, ok := ctx.Value(ContextKeyDefenderRunner).(DefenderRunner)
	return r, ok && r != nil
}
