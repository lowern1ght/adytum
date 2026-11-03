package adytum

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lowern1ght/adytum/internal/envfile"
	"github.com/lowern1ght/adytum/internal/sopsconf"
	"github.com/lowern1ght/adytum/internal/store"
	"gopkg.in/yaml.v3"
)

// Run выполняет команду с секретами, подставленными в окружение.
//
// Каждый env-файл расшифровывается, его KEY=VALUE добавляются в
// окружение процесса. Значения никогда не печатаются.
func (a *App) Run(envs []string, command []string) error {
	if len(envs) == 0 {
		return fmt.Errorf("%w: нужен хотя бы один --env", ErrUsage)
	}
	environment := os.Environ()
	for _, target := range envs {
		project, service, err := SplitTarget(target)
		if err != nil {
			return err
		}
		path, _, err := ResolveSecret(a.Root, project, service)
		if err != nil {
			return err
		}
		plain, err := a.readPlaintext(path)
		if err != nil {
			return err
		}
		values, err := envValues(path, plain)
		if err != nil {
			return err
		}
		for key, value := range values {
			environment = setEnv(environment, key, value)
		}
	}

	process := exec.Command(command[0], command[1:]...)
	process.Env = environment
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	if err := process.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("команда завершилась с кодом %d", exit.ExitCode())
		}
		return err
	}
	return nil
}

// envValues извлекает пары KEY=VALUE из расшифрованного секрета.
//
// Формат определяется расширением файла: .env разбирается как
// KEY=VALUE, .yaml и .json — как отображение, значения которого
// приводятся к строке.
func envValues(path string, plain []byte) (map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
		return structuredValues(path, plain)
	default:
		entries, err := envfile.Parse(plain)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		values := make(map[string]string, len(entries))
		for _, entry := range entries {
			values[entry.Key] = entry.Value
		}
		return values, nil
	}
}

// structuredValues разбирает yaml/json-секрет в пары KEY=VALUE.
func structuredValues(path string, plain []byte) (map[string]string, error) {
	var document map[string]any
	if err := yaml.Unmarshal(plain, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	values := make(map[string]string, len(document))
	for key, value := range document {
		values[key] = scalarString(value)
	}
	return values, nil
}

// scalarString приводит значение документа к строке.
func scalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// setEnv заменяет или добавляет переменную окружения.
func setEnv(environment []string, key, value string) []string {
	prefix := key + "="
	for index, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			environment[index] = prefix + value
			return environment
		}
	}
	return append(environment, prefix+value)
}

// StatusJSON печатает состояние репозитория.
//
// При jsonOutput=true вывод идёт в формате JSON, иначе — текстом.
func (a *App) StatusJSON(jsonOutput bool) error {
	branch, _ := a.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	remote, _ := a.gitOutput("remote", "get-url", "origin")

	recipients := []string{}
	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err == nil {
		config, parseErr := sopsconf.Parse(content)
		if parseErr == nil {
			recipients = config.Recipients()
		}
	}

	unlock, unlockErr := store.Unlock(a.Now())
	unlockActive := unlockErr == nil && unlock.Unlocked
	unlockExpires := ""
	if unlockErr == nil && !unlock.ExpiresAt.IsZero() {
		unlockExpires = unlock.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
	}

	filters := map[string]string{}
	for _, key := range []string{"filter.adytum.clean", "filter.adytum.smudge", "filter.adytum.required"} {
		value, _ := a.gitOutput("config", "--get", key)
		filters[key] = value
	}

	if !jsonOutput {
		return a.Status()
	}

	payload := map[string]any{
		"branch":     branch,
		"remote":     remote,
		"recipients": recipients,
		"unlock": map[string]any{
			"active":     unlockActive,
			"expires_at": unlockExpires,
		},
		"filters": filters,
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

// listSecretPaths возвращает относительные пути файлов секретов.
func (a *App) listSecretPaths() ([]string, error) {
	paths, err := a.secretPaths()
	if err != nil {
		return nil, err
	}
	relative := make([]string, 0, len(paths))
	for _, path := range paths {
		relative = append(relative, filepath.ToSlash(relativeTo(a.Root, path)))
	}
	return relative, nil
}
