package fsm

// Flow Constants
const (
	FlowRegistration = "registration.yaml"
	FlowMainMenu     = "main_menu.yaml"
	FlowSettings     = "settings.yaml"
)

// State Constants
const (
	StateSelectLanguage = "SELECT_LANGUAGE"
	StateStart          = "START"
	StateInputLogin     = "INPUT_LOGIN"
	StateMainMenu       = "MAIN_MENU"
	StateSettingsMenu   = "SETTINGS_MENU"
)

// State Types
const (
	StateTypeInteractive = "interactive"
	StateTypeSystem      = "system"
	StateTypeInput       = "input"
	StateTypeFinal       = "final"
)

// Variable Keys
const (
	VarS21Login      = "{s21_login}"
	VarLevel         = "{level}"
	VarCoalition     = "{coalition}"
	VarLanguageFlag  = "{language_flag}" // Preferred
	VarLanguageFlag2 = "{languageflag}"  // Legacy support
)

// Input Constants
const (
	InputSetRu = "set_ru"
	InputSetEn = "set_en"
)

// Language Constants
const (
	LangRu = "ru"
	LangEn = "en"
)

// Default Values
const (
	DefaultS21Login  = "student"
	DefaultLevel     = "0"
	DefaultCoalition = "None"
	DefaultFlagRu    = "🇷🇺"
	DefaultFlagEn    = "🇺🇸"
)
