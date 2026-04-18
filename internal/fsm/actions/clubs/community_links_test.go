package clubs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadBotLinks_FromCommunityLinksFile(t *testing.T) {
	const testVariousPath = "bot_content/various"

	tmpDir := t.TempDir()
	t.Setenv("GIT_LOCAL_PATH", tmpDir)

	targetPath := filepath.Join(tmpDir, testVariousPath, communityLinksFilename)
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o755))
	require.NoError(t, os.WriteFile(targetPath, []byte(`content_links:
  bot_links:
    telegram: "https://t.me/custom_group"
  repo_links:
    bot: "https://github.com/my-group/noemx21-bot"
    bot_content: "https://gitlab.com/my-group/noemx21-content"
`), 0o644))

	botTelegramLink, botContentRepoLink, botRepoLink := loadBotLinks(testVariousPath)
	require.Equal(t, "https://t.me/custom_group", botTelegramLink)
	require.Equal(t, "https://gitlab.com/my-group/noemx21-content", botContentRepoLink)
	require.Equal(t, "https://github.com/my-group/noemx21-bot", botRepoLink)
}

func TestLoadBotLinks_DefaultsWhenFileMissing(t *testing.T) {
	t.Setenv("GIT_LOCAL_PATH", t.TempDir())

	botTelegramLink, botContentRepoLink, botRepoLink := loadBotLinks("")
	require.Equal(t, defaultBotTelegramLink, botTelegramLink)
	require.Equal(t, defaultBotContentRepoLink, botContentRepoLink)
	require.Equal(t, defaultBotRepoLink, botRepoLink)
}
