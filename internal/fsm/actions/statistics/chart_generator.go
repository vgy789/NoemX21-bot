package statistics

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vgy789/noemx21-bot/internal/pkg/charts"
)

const (
	// Chart dimensions
	chartWidth  = 1000 // Ширина графика в пикселях
	chartHeight = 800  // Высота графика в пикселях

	// Chart styling
	chartFontSize = 8.0 // Размер шрифта на графике
	chartPadding  = 100 // Отступы вокруг графика (Top, Right, Bottom, Left)

	// Chart scaling and thresholds
	defaultGlobalMaxValue = 3000.0 // Дефолтное максимальное значение для навыков при отсутствии данных
	minGlobalMaxValue     = 1.0    // Минимальное значение для globalMax (избегаем деления на ноль)
	globalMinValue        = 0.0    // Минимальное значение для навыков (всегда 0)
	minLogDiffValue       = 1.0    // Минимальное значение для logDiff (избегаем деления на ноль)
	activeSkillThreshold  = 5.0    // Порог в процентах для отображения навыка в сравнении (>= 5%)
	indicatorMaxValue     = 100.0  // Максимальное значение для индикаторов (шкала 0-100)

	// File system (chartTempDir set from config in Register)
	chartDirPerms     = 0755
	chartFilePerms    = 0644                                     // Права доступа для записи файла графика
	chartFilenameFmt  = "skills_radar_%s.png"                    // Формат имени файла графика
	chartTimeFormat   = "2006-01-02_15-04-05.000"                // Формат временной метки в имени файла
	chartTitleCompare = "Skills Comparison\n(logarithmic scale)" // Заголовок графика сравнения
	chartTitleProfile = "Skills Profile: %s"                     // Шаблон заголовка профиля навыков
)

// chartTempDir is the directory for temporary chart files; set from config (CHART_TEMP_DIR) in Register.
var chartTempDir = "tmp"

// SetChartTempDir sets the chart temp directory from config. Called by Register.
func SetChartTempDir(dir string) {
	if dir != "" {
		chartTempDir = dir
	}
}

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

var (
	chartCache      = make(map[uint64]string)
	chartCacheMutex sync.Mutex
)

func generateRadarChart(usersData map[string]map[string]int32, orderedLogins []string) (string, error) {
	if len(usersData) == 0 {
		return "", fmt.Errorf("no data provided for chart")
	}

	// Calculate hash of input data for caching. Use provided ordering when available
	h := fnv.New64a()
	var keysOrder []string
	if len(orderedLogins) != 0 {
		keysOrder = make([]string, 0, len(orderedLogins))
		for _, l := range orderedLogins {
			if _, ok := usersData[l]; ok {
				keysOrder = append(keysOrder, l)
			}
		}
	}
	if len(keysOrder) == 0 {
		for l := range usersData {
			keysOrder = append(keysOrder, l)
		}
		sort.Strings(keysOrder)
	}
	for _, l := range keysOrder {
		h.Write([]byte(l))
		skills := usersData[l]
		var sortedSkills []string
		for s := range skills {
			sortedSkills = append(sortedSkills, s)
		}
		sort.Strings(sortedSkills)
		for _, s := range sortedSkills {
			h.Write([]byte(s))
			if err := binary.Write(h, binary.LittleEndian, skills[s]); err != nil {
				return "", fmt.Errorf("failed to write hash data: %w", err)
			}
		}
	}
	dataHash := h.Sum64()

	chartCacheMutex.Lock()
	if path, ok := chartCache[dataHash]; ok {
		// Check if file still exists
		if _, err := os.Stat(path); err == nil {
			chartCacheMutex.Unlock()
			return path, nil
		}
	}
	chartCacheMutex.Unlock()

	// Respect provided order when possible so `me` can be first
	var logins []string
	if len(orderedLogins) != 0 {
		for _, l := range orderedLogins {
			if _, ok := usersData[l]; ok {
				logins = append(logins, l)
			}
		}
	}
	if len(logins) == 0 {
		for l := range usersData {
			logins = append(logins, l)
		}
		sort.Strings(logins)
	}

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
		globalMax = defaultGlobalMaxValue
	}

	// Ensure max is at least slightly above 0 to avoid division by zero
	if globalMax == 0 {
		globalMax = minGlobalMaxValue
	}

	// Global Min is always 0 for skills
	globalMin := globalMinValue

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
		logDiff = minLogDiffValue
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
			scaledVal := ((logVal - logMin) / logDiff) * indicatorMaxValue

			if val > 0 && scaledVal >= activeSkillThreshold {
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
			scaledVal := ((logVal - logMin) / logDiff) * indicatorMaxValue
			userValues = append(userValues, scaledVal)
		}
		values = append(values, userValues)
	}

	// Indicators use 0-100 scale
	var indicatorMaxValues []float64
	for range activeSkills {
		indicatorMaxValues = append(indicatorMaxValues, indicatorMaxValue)
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

	chartTitle := chartTitleCompare
	if isIndividual {
		chartTitle = fmt.Sprintf(chartTitleProfile, logins[0])
	}

	// If this is a 2-series comparison, force colors: first = blue, second = red
	opts := []charts.OptionFunc{
		charts.TitleTextOptionFunc(chartTitle),
		charts.LegendLabelsOptionFunc(logins),
		charts.RadarIndicatorOptionFunc(activeSkills, indicatorMaxValues),
		charts.RadarDomainsOptionFunc(radarDomains),
		charts.WidthOptionFunc(chartWidth),
		charts.HeightOptionFunc(chartHeight),
		charts.FontSizeOptionFunc(chartFontSize),
		charts.PaddingOptionFunc(charts.Box{Top: chartPadding, Right: chartPadding, Bottom: chartPadding, Left: chartPadding}), // Padding for domain labels
	}
	if len(logins) == 2 {
		// Add or overwrite a theme that uses blue for first series and red for second.
		// Build the theme option from the default light theme so text/background colors
		// stay usable (otherwise missing TextColor makes labels invisible).
		themeName := "user_peer"
		base := charts.NewTheme(charts.ThemeLight)
		charts.AddTheme(themeName, charts.ThemeOption{
			IsDarkMode:         base.IsDark(),
			AxisStrokeColor:    base.GetAxisStrokeColor(),
			AxisSplitLineColor: base.GetAxisSplitLineColor(),
			BackgroundColor:    base.GetBackgroundColor(),
			TextColor:          base.GetTextColor(),
			SeriesColors: []charts.Color{
				charts.ParseColor("#5470c6"), // blue (you)
				charts.ParseColor("#ee6666"), // red  (peer)
			},
		})
		opts = append(opts, charts.ThemeOptionFunc(themeName))
	}

	// Generate chart
	p, err := charts.RadarRender(values, opts...)
	if err != nil {
		return "", err
	}

	buf, err := p.Bytes()
	if err != nil {
		return "", err
	}

	// Save to file
	filename := fmt.Sprintf(chartFilenameFmt, time.Now().Format(chartTimeFormat))
	if err := os.MkdirAll(chartTempDir, chartDirPerms); err != nil {
		return "", err
	}

	filePath := filepath.Join(chartTempDir, filename)
	if err := os.WriteFile(filePath, buf, chartFilePerms); err != nil {
		return "", err
	}

	chartCacheMutex.Lock()
	chartCache[dataHash] = filePath
	chartCacheMutex.Unlock()

	return filePath, nil
}

// GenerateRadarChartFromData is an exported wrapper to allow other packages
// to generate a radar chart from provided skills data. `orderedLogins` can
// be used to enforce ordering (e.g., current user first, peer second).
func GenerateRadarChartFromData(usersData map[string]map[string]int32, orderedLogins []string) (string, error) {
	return generateRadarChart(usersData, orderedLogins)
}
