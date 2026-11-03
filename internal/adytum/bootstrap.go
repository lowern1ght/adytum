package adytum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lowern1ght/adytum/internal/store"
)

// GitHubAPI — адрес API GitHub.
const GitHubAPI = "https://api.github.com"

// SecretsRepo — имя приватного репозитория с данными.
//
// Репозиторий данных отделён от репозитория инструмента: в нём лежит
// только шифротекст секретов.
const SecretsRepo = "secrets"

// BootstrapResult — результат развёртывания приватного репозитория.
type BootstrapResult struct {
	// Owner — владелец репозитория.
	Owner string `json:"owner"`
	// Repo — имя репозитория.
	Repo string `json:"repo"`
	// URL — адрес репозитория.
	URL string `json:"url"`
	// Private — репозиторий приватный.
	Private bool `json:"private"`
	// Created — репозиторий создан (false, если уже существовал).
	Created bool `json:"created"`
	// Pushed — изменения отправлены в remote.
	Pushed bool `json:"pushed"`
}

// TokenStore — источник токена GitHub.
//
// Токен берётся из ~/.secrets/personal/github.env (ключ GITHUB_TOKEN)
// либо из ~/.adytum/github-token. Значение никогда не печатается.
type TokenStore struct {
	// Home — каталог состояния adytum.
	Home string
	// LegacyDir — каталог старого plaintext-хранилища.
	LegacyDir string
}

// Token возвращает токен GitHub.
//
// Порядок источников: ~/.adytum/github-token, затем
// ~/.secrets/personal/github.env.
func (t TokenStore) Token() (string, error) {
	if t.Home != "" {
		path := filepath.Join(t.Home, "github-token")
		if content, err := os.ReadFile(path); err == nil {
			if token := strings.TrimSpace(string(content)); token != "" {
				return token, nil
			}
		}
	}
	if t.LegacyDir != "" {
		path := filepath.Join(t.LegacyDir, "personal", "github.env")
		if content, err := os.ReadFile(path); err == nil {
			if token := tokenFromEnv(string(content)); token != "" {
				return token, nil
			}
		}
	}
	return "", fmt.Errorf("токен GitHub не найден: положите его в %s или %s",
		filepath.Join(t.Home, "github-token"),
		filepath.Join(t.LegacyDir, "personal", "github.env"))
}

// tokenFromEnv извлекает GITHUB_TOKEN из содержимого env-файла.
func tokenFromEnv(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "GITHUB_TOKEN") {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

// GitHubClient — клиент API GitHub.
type GitHubClient struct {
	// BaseURL — адрес API (для тестов подменяется).
	BaseURL string
	// HTTP — HTTP-клиент.
	HTTP *http.Client
}

// githubUser — ответ GET /user.
type githubUser struct {
	Login string `json:"login"`
}

// CurrentUser возвращает логин владельца токена.
func (c *GitHubClient) CurrentUser(ctx context.Context, token string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"/user", nil)
	if err != nil {
		return "", err
	}
	c.authorize(request, token)
	response, err := c.http().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API /user: %s", response.Status)
	}
	var user githubUser
	if err := json.Unmarshal(body, &user); err != nil {
		return "", err
	}
	if user.Login == "" {
		return "", fmt.Errorf("GitHub API /user: пустой login")
	}
	return user.Login, nil
}

// CreateRepo создаёт приватный репозиторий.
//
// Возвращает false, если репозиторий уже существует (HTTP 422).
func (c *GitHubClient) CreateRepo(ctx context.Context, token, name string) (bool, error) {
	payload, err := json.Marshal(map[string]any{"name": name, "private": true})
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/user/repos", strings.NewReader(string(payload)))
	if err != nil {
		return false, err
	}
	c.authorize(request, token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http().Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	switch response.StatusCode {
	case http.StatusCreated, http.StatusOK:
		return true, nil
	case http.StatusUnprocessableEntity:
		// Репозиторий уже есть — это не ошибка.
		return false, nil
	default:
		return false, fmt.Errorf("GitHub API /user/repos: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
}

// authorize добавляет заголовок авторизации.
func (c *GitHubClient) authorize(request *http.Request, token string) {
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "adytum")
}

// baseURL возвращает адрес API.
func (c *GitHubClient) baseURL() string {
	if c.BaseURL == "" {
		return GitHubAPI
	}
	return c.BaseURL
}

// http возвращает HTTP-клиент.
func (c *GitHubClient) http() *http.Client {
	if c.HTTP == nil {
		return &http.Client{Timeout: 15 * time.Second}
	}
	return c.HTTP
}

// BootstrapRemote создаёт приватный репозиторий данных и настраивает remote.
//
// Токен используется только в памяти: он не попадает в git config и не
// сохраняется на диск. Push выполняется через одноразовый URL.
func (a *App) BootstrapRemote() (BootstrapResult, error) {
	tokenStore, err := a.tokenStore()
	if err != nil {
		return BootstrapResult{}, err
	}
	token, err := tokenStore.Token()
	if err != nil {
		return BootstrapResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := &GitHubClient{}
	owner, err := client.CurrentUser(ctx, token)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("не удалось определить владельца по токену: %w", err)
	}
	created, err := client.CreateRepo(ctx, token, SecretsRepo)
	if err != nil {
		return BootstrapResult{}, err
	}

	remoteURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, SecretsRepo)
	result := BootstrapResult{
		Owner:   owner,
		Repo:    SecretsRepo,
		URL:     remoteURL,
		Private: true,
		Created: created,
	}

	if err := a.ensureRemote(remoteURL); err != nil {
		return result, err
	}
	pushed, err := a.pushWithToken(owner, token, remoteURL)
	result.Pushed = pushed
	if err != nil {
		return result, err
	}
	return result, nil
}

// ensureRemote настраивает origin на адрес репозитория данных.
//
// Если origin уже настроен на другой адрес, он переписывается: цель
// bootstrap — привести локальную копию к каноническому адресу
// репозитория данных.
func (a *App) ensureRemote(url string) error {
	existing, err := a.gitOutput("remote", "get-url", "origin")
	if err != nil || existing == "" {
		if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "remote", "add", "origin", url); err != nil {
			return fmt.Errorf("не удалось добавить remote: %w", err)
		}
		return nil
	}
	if existing == url {
		return nil
	}
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "remote", "set-url", "origin", url); err != nil {
		return fmt.Errorf("не удалось перенастроить remote: %w", err)
	}
	a.printf("remote origin: %s → %s\n", existing, url)
	return nil
}

// pushWithToken отправляет изменения через одноразовый URL с токеном.
//
// Токен не сохраняется: URL с токеном передаётся только в аргументах
// команды push. Текст ошибки очищается от токена — git печатает URL в
// сообщениях об отказе по авторизации.
func (a *App) pushWithToken(owner, token, remoteURL string) (bool, error) {
	branch, err := a.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return false, err
	}
	authURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", url.PathEscape(token), owner, SecretsRepo)
	// credential.helper= запрещает git обращаться к помощнику учётных
	// данных и сохранять токен.
	refspec := fmt.Sprintf("HEAD:refs/heads/%s", branch)
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "-c", "credential.helper=", "push", authURL, refspec); err != nil {
		return false, fmt.Errorf("не удалось отправить изменения: %w", redactToken(err, token))
	}
	return true, nil
}

// redactToken убирает токен из текста ошибки.
//
// Git печатает URL с учётными данными в сообщениях об отказе по
// авторизации: без очистки токен попал бы в вывод команды.
func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), token, "***")
	return errors.New(message)
}

// tokenStore собирает источник токена GitHub.
func (a *App) tokenStore() (TokenStore, error) {
	home, err := store.Home()
	if err != nil {
		return TokenStore{}, err
	}
	legacy := os.Getenv("ADYTUM_LEGACY_SECRETS_DIR")
	if legacy == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			legacy = filepath.Join(userHome, ".secrets")
		}
	}
	return TokenStore{Home: home, LegacyDir: legacy}, nil
}
