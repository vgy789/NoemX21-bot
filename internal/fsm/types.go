package fsm

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
}

type Interface struct {
	Text    map[string]string `yaml:"text"` // Locale -> Text
	Buttons []Button          `yaml:"buttons"`
}

type Button struct {
	ID        string      `yaml:"id"`
	Label     interface{} `yaml:"label"`      // String or Map[string]string
	NextState string      `yaml:"next_state"` // can be "STATE" or "file.yaml/STATE"
}

type Transition struct {
	Condition string `yaml:"condition"`
	NextState string `yaml:"next_state"`
	Trigger   string `yaml:"trigger"`
}

type Logic struct {
	Check   string                 `yaml:"check"`
	Action  string                 `yaml:"action"`
	Payload map[string]interface{} `yaml:"payload"`
}

// UserState represents the current state of a user.
type UserState struct {
	UserID       int64                  `json:"user_id"`
	CurrentFlow  string                 `json:"current_flow"`  // e.g. "registration.yaml"
	CurrentState string                 `json:"current_state"` // e.g. "AWAITING_OTP"
	Context      map[string]interface{} `json:"context"`       // Store arbitrary data
	Language     string                 `json:"language"`      // "ru" or "en"
}
