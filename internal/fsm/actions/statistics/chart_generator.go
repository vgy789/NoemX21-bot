package statistics

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vgy789/noemx21-bot/internal/pkg/charts"
)

type skillDomain struct {
	Name   string
	Skills []string
	Color  string
}

var skillDomains = []skillDomain{
	{
		Name:  "Fundamentals & Low-level",
		Color: "#5470c6", // Blue
		Skills: []string{
			"Math", "Algorithms", "Types and Data Structures",
			"Structured Programming", "C", "C++", "Parallel Computing",
		},
	},
	{
		Name:  "System & DevOps",
		Color: "#91cc75", // Green
		Skills: []string{
			"Linux", "Shell/Bash", "Windows", "Powershell",
			"Network & System Admin", "Network Architecture",
			"Systems Integration", "DevOps",
		},
	},
	{
		Name:  "InfoSec",
		Color: "#ee6666", // Red
		Skills: []string{
			"Information Security", "Cryptography", "Regulatory Docs & Standards",
			"Network Attacks", "Web Security", "Social Engineering", "Physical Access",
		},
	},
	{
		Name:  "Back-end & Data",
		Color: "#73c0de", // Light Blue
		Skills: []string{
			"Software Architecture", "OOP", "Functional Programming",
			"Java", "C#", "Go", "SQL", "DB & Data", "Python", "ML & AI",
		},
	},
	{
		Name:  "Web & Mobile",
		Color: "#fac858", // Yellow/Orange
		Skills: []string{
			"Web", "HTML/CSS", "Frontend Basics", "Frontend",
			"JavaScript", "TypeScript", "Mobile", "Kotlin", "Swift",
		},
	},
	{
		Name:  "Design & Graphics",
		Color: "#ea7ccc", // Pink
		Skills: []string{
			"Graphics", "3D Modeling", "UI & Design Tools", "UX & Design Tools",
		},
	},
	{
		Name:  "Quality & Analysis",
		Color: "#3ba272", // Dark Green
		Skills: []string{
			"QA", "Code Review", "Research", "Analysis",
			"Analytical Thinking", "Requirements Analysis", "Business Modeling",
		},
	},
	{
		Name:  "Management & Soft Skills",
		Color: "#fc8452", // Orange
		Skills: []string{
			"Project Planning", "Project Management", "Change Management",
			"Leadership", "Team Work", "Company Experience", "Copywriting",
		},
	},
}

// orderedSkills defines the fixed order of skills for the radar chart
var orderedSkills = []string{
	// 1. Фундаментальные знания и Low-level
	"Math",
	"Algorithms",
	"Types and Data Structures",
	"Structured Programming",
	"C",
	"C++",
	"Parallel Computing",

	// 2. Системное администрирование, Сети и DevOps
	"Linux",
	"Shell/Bash",
	"Windows",
	"Powershell",
	"Network & System Admin",
	"Network Architecture",
	"Systems Integration",
	"DevOps",

	// 3. Информационная безопасность (InfoSec)
	"Information Security",
	"Cryptography",
	"Regulatory Docs & Standards",
	"Network Attacks",
	"Web Security",
	"Social Engineering",
	"Physical Access",

	// 4. Архитектура, Бэкенд и Данные
	"Software Architecture",
	"OOP",
	"Functional Programming",
	"Java",
	"C#",
	"Go",
	"SQL",
	"DB & Data",
	"Python",
	"ML & AI",

	// 5. Web-разработка (Frontend) и Мобайл
	"Web",
	"HTML/CSS",
	"Frontend Basics",
	"Frontend",
	"JavaScript",
	"TypeScript",
	"Mobile",
	"Kotlin",
	"Swift",

	// 6. Дизайн и Графика
	"Graphics",
	"3D Modeling",
	"UI & Design Tools",
	"UX & Design Tools",

	// 7. Качество, Аналитика и Процессы
	"QA",
	"Code Review",
	"Research",
	"Analysis",
	"Analytical Thinking",
	"Requirements Analysis",
	"Business Modeling",

	// 8. Менеджмент и Soft Skills
	"Project Planning",
	"Project Management",
	"Change Management",
	"Leadership",
	"Team Work",
	"Company Experience",
	"Copywriting",
}

func generateRadarChart(usersData map[string]map[string]int32) (string, error) {
	if len(usersData) == 0 {
		return "", fmt.Errorf("no data provided for chart")
	}

	// Sort logins to ensure consistent color assignment
	var logins []string
	for login := range usersData {
		logins = append(logins, login)
	}
	sort.Strings(logins)

	var values [][]float64
	var globalMax = -1e18
	var hasData bool

	// Find global max across all users and all skills
	for _, skills := range usersData {
		for _, valInt := range skills {
			val := float64(valInt)
			if val > globalMax {
				globalMax = val
			}
			hasData = true
		}
	}

	// Default if no data
	if !hasData {
		globalMax = 3000
	}

	// Ensure max is at least slightly above 0 to avoid division by zero
	if globalMax == 0 {
		globalMax = 1
	}

	// Global Min is always 0 for skills
	var globalMin float64 = 0

	// Step 1: Create normalized maps for all users for efficient lookup
	userNormalizedSkills := make(map[string]map[string]int32)
	for login, skills := range usersData {
		normalized := make(map[string]int32)
		for k, v := range skills {
			normalized[strings.ToLower(k)] = v
		}
		userNormalizedSkills[login] = normalized
	}

	// Use logarithmic scaling to handle outliers (like 'Company Experience')
	// Formula: scaledVal = (log(val+1) - log(min+1)) / (log(max+1) - log(min+1)) * 100
	logMin := math.Log10(globalMin + 1)
	logMax := math.Log10(globalMax + 1)
	logDiff := logMax - logMin
	if logDiff == 0 {
		logDiff = 1
	}

	// Step 2: Determine active skills (not 0 and >= 5% for at least one user)
	var activeSkills []string
	isIndividual := len(logins) == 1
	for _, skillName := range orderedSkills {
		if isIndividual {
			activeSkills = append(activeSkills, skillName)
			continue
		}

		keep := false
		for _, login := range logins {
			skills := usersData[login]
			normalized := userNormalizedSkills[login]

			valInt, ok := skills[skillName]
			if !ok {
				valInt = normalized[strings.ToLower(skillName)]
			}
			val := float64(valInt)

			logVal := math.Log10(val + 1)
			scaledVal := ((logVal - logMin) / logDiff) * 100.0

			if val > 0 && scaledVal >= 5.0 {
				keep = true
				break
			}
		}
		if keep {
			activeSkills = append(activeSkills, skillName)
		}
	}

	// Step 3: Prepare data for active skills only
	for _, login := range logins {
		skills := usersData[login]
		normalized := userNormalizedSkills[login]

		var userValues []float64
		for _, skillName := range activeSkills {
			valInt, ok := skills[skillName]
			if !ok {
				valInt = normalized[strings.ToLower(skillName)]
			}

			val := float64(valInt)
			logVal := math.Log10(val + 1)
			scaledVal := ((logVal - logMin) / logDiff) * 100.0
			userValues = append(userValues, scaledVal)
		}
		values = append(values, userValues)
	}

	// Indicators use 0-100 scale
	var indicatorMaxValues []float64
	for range activeSkills {
		indicatorMaxValues = append(indicatorMaxValues, 100.0)
	}

	// Step 4: Calculate radar domains for active skills
	var radarDomains []charts.RadarDomain
	skillIndexMap := make(map[string]int)
	for i, s := range activeSkills {
		skillIndexMap[s] = i
	}

	for _, domain := range skillDomains {
		firstIdx := -1
		lastIdx := -1
		for _, s := range domain.Skills {
			if idx, ok := skillIndexMap[s]; ok {
				if firstIdx == -1 {
					firstIdx = idx
				}
				lastIdx = idx
			}
		}

		if firstIdx != -1 {
			radarDomains = append(radarDomains, charts.RadarDomain{
				Name:  domain.Name,
				Start: firstIdx,
				End:   lastIdx,
				Color: charts.ParseColor(domain.Color),
			})
		}
	}

	chartTitle := "Skills Comparison\n(logarithmic scale)"
	if isIndividual {
		chartTitle = fmt.Sprintf("Skills Profile: %s", logins[0])
	}

	// Generate chart
	p, err := charts.RadarRender(
		values,
		charts.TitleTextOptionFunc(chartTitle),
		charts.LegendLabelsOptionFunc(logins),
		charts.RadarIndicatorOptionFunc(activeSkills, indicatorMaxValues),
		charts.RadarDomainsOptionFunc(radarDomains),
		charts.WidthOptionFunc(1000),
		charts.HeightOptionFunc(800),
		charts.FontSizeOptionFunc(8.0),
		charts.PaddingOptionFunc(charts.Box{Top: 100, Right: 100, Bottom: 100, Left: 100}), // Padding for domain labels
	)
	if err != nil {
		return "", err
	}

	buf, err := p.Bytes()
	if err != nil {
		return "", err
	}

	// Save to file
	filename := fmt.Sprintf("skills_radar_%s.png", time.Now().Format("2006-01-02_15-04-05.000"))
	tmpPath := "tmp"
	if err := os.MkdirAll(tmpPath, 0755); err != nil {
		return "", err
	}

	filePath := filepath.Join(tmpPath, filename)
	if err := os.WriteFile(filePath, buf, 0644); err != nil {
		return "", err
	}

	return filePath, nil
}
