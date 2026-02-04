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
	VarS21Login       = "{my_s21login}"
	VarLevel          = "{my_level}"
	VarCoalition      = "{my_coalition}"
	VarLanguageFlag   = "{my_lang_emoji}"
	VarCampus         = "{my_campus}"
	VarAvailableCount = "{available_count}"
	VarExp            = "{my_exp}"
	VarPrps           = "{my_prps}"
	VarCrps           = "{my_crps}"
	VarCoins          = "{my_coins}"
	VarDate           = "{current_date}"
)

// DefaultVariables map acts as a single source of truth for default values (debugging/fallback)
var DefaultVariables = map[string]string{
	VarS21Login:       "jonnabin",
	VarLevel:          "99",
	VarCoalition:      "Abcdefg",
	VarLanguageFlag:   DefaultFlagRu,
	VarCampus:         "Abcdefg",
	VarAvailableCount: "987",
	VarExp:            "987654",
	VarPrps:           "98",
	VarCrps:           "98",
	VarCoins:          "987",
	VarDate:           "03.02.3333",
}

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
	DefaultLanguage = LangRu
	DefaultFlagRu   = "🇷🇺"
	DefaultFlagEn   = "🇺🇸"
)

// GetDefaultVariables returns a map of variables with defaults, adjusted for language.
func GetDefaultVariables(lang string) map[string]string {
	vars := make(map[string]string, len(DefaultVariables))
	for k, v := range DefaultVariables {
		vars[k] = v
	}

	// Adjust language-specific defaults
	if lang == LangEn {
		vars[VarLanguageFlag] = DefaultFlagEn
		vars[VarCampus] = "Unknown campus"
		vars[VarCoalition] = "No coalition"
		vars[VarS21Login] = "Guest"
	} else {
		vars[VarLanguageFlag] = DefaultFlagRu
		vars[VarCampus] = "Неизвестный кампус"
		vars[VarCoalition] = "Нет коалиции"
		vars[VarS21Login] = "Гость"
	}

	return vars
}
