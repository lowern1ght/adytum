package adytum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTokenStoreReadsEnvFile проверяет чтение токена из env-файла.
//
// Формат файла — KEY=VALUE, как у всего остального хранилища.
func TestTokenStoreReadsEnvFile(t *testing.T) {
	home := t.TempDir()
	legacy := t.TempDir()
	writeFile(t, filepath.Join(legacy, "personal", "github.env"), "GITHUB_TOKEN=ghp_from_env\n")

	store := TokenStore{Home: home, LegacyDir: legacy}

	token, err := store.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "ghp_from_env" {
		t.Errorf("токен = %q, ожидалось ghp_from_env", token)
	}
}

// TestTokenStorePrefersLocalFile проверяет приоритет файла в каталоге
// adytum над старым хранилищем.
func TestTokenStorePrefersLocalFile(t *testing.T) {
	home := t.TempDir()
	legacy := t.TempDir()
	writeFile(t, filepath.Join(home, "github-token"), "ghp_local\n")
	writeFile(t, filepath.Join(legacy, "personal", "github.env"), "GITHUB_TOKEN=ghp_legacy\n")

	store := TokenStore{Home: home, LegacyDir: legacy}

	token, err := store.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "ghp_local" {
		t.Errorf("токен = %q, ожидалось ghp_local", token)
	}
}

// TestTokenStoreReportsMissingToken проверяет внятную ошибку при
// отсутствии токена.
//
// Молчаливая ошибка заставила бы искать токен вручную.
func TestTokenStoreReportsMissingToken(t *testing.T) {
	store := TokenStore{Home: t.TempDir(), LegacyDir: t.TempDir()}

	_, err := store.Token()
	if err == nil {
		t.Fatal("ожидалась ошибка отсутствия токена")
	}
	if !strings.Contains(err.Error(), "github-token") {
		t.Errorf("ошибка = %q, ожидалось упоминание файла токена", err.Error())
	}
}

// TestTokenStoreIgnoresEmptyFile проверяет, что пустой файл токена не
// считается токеном.
func TestTokenStoreIgnoresEmptyFile(t *testing.T) {
	home := t.TempDir()
	legacy := t.TempDir()
	writeFile(t, filepath.Join(home, "github-token"), "  \n")
	writeFile(t, filepath.Join(legacy, "personal", "github.env"), "GITHUB_TOKEN=ghp_legacy\n")

	store := TokenStore{Home: home, LegacyDir: legacy}

	token, err := store.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "ghp_legacy" {
		t.Errorf("токен = %q, ожидалось ghp_legacy", token)
	}
}

// TestTokenFromEnvIgnoresOtherKeys проверяет, что читается именно
// GITHUB_TOKEN.
func TestTokenFromEnvIgnoresOtherKeys(t *testing.T) {
	content := "OTHER=zzz\nGITHUB_TOKEN=ghp_right\n"

	if got := tokenFromEnv(content); got != "ghp_right" {
		t.Errorf("токен = %q, ожидалось ghp_right", got)
	}
}

// TestTokenFromEnvStripsQuotes проверяет снятие кавычек вокруг
// значения: в env-файлах значение часто заключено в кавычки.
func TestTokenFromEnvStripsQuotes(t *testing.T) {
	if got := tokenFromEnv(`GITHUB_TOKEN="ghp_quoted"`); got != "ghp_quoted" {
		t.Errorf("токен = %q, ожидалось ghp_quoted", got)
	}
}

// TestSecretsRepoName проверяет имя репозитория данных.
//
// Имя фиксировано: переименование репозитория на GitHub ломает
// ссылки и скрипты, завязанные на адрес.
func TestSecretsRepoName(t *testing.T) {
	if SecretsRepo != "secrets" {
		t.Errorf("SecretsRepo = %q, ожидалось secrets", SecretsRepo)
	}
}

// writeFile создаёт файл с указанным содержимым.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
