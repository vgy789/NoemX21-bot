package gitsync

type ClubLeaderYAML struct {
	Name     string `yaml:"name"`
	FormLink string `yaml:"form_link"`
}

type ClubYAML struct {
	ID           int    `yaml:"id"`
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	ExternalLink string `yaml:"external_link"`
	LeaderLogin  string `yaml:"leader_login"`
	Campus       string `yaml:"campus"`
	Category     string `yaml:"category"`
	IsLocal      *bool  `yaml:"is_local"`
	IsActive     *bool  `yaml:"is_active"`
}

type ClubsFileYAML struct {
	Leader ClubLeaderYAML `yaml:"leader"`
	Clubs  []ClubYAML     `yaml:"clubs"`
}
