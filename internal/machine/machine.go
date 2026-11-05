// Package machine хранит профиль машины: какие разделы она
// расшифровывает, какой у неё роль (pc или headless), работает ли она
// только на чтение и насколько живёт маркер разблокировки.
//
// Настройки лежат в ~/.adytum/config в формате yaml. Файл правит
// владелец руками: роль машины и нужные ей разделы не выводятся из
// меток машин.
//
// Одна машина — один файл настроек: разные файлы со временем начинают
// описывать разные машины, и половина правил применяется не к той
// машине.
package machine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lowern1ght/adytum/internal/store"
	"gopkg.in/yaml.v3"
)

// FileName — имя файла настроек в каталоге состояния adytum.
const FileName = "config"

// Role — профиль машины.
//
// Профиль определяет, какие проверки применимы: на pc-машине таймер
// systemd не нужен, на headless-машине не нужен интерактивный ввод.
type Role string

// Профили машины.
const (
	// RoleUnknown — профиль не задан: определяется по окружению.
	RoleUnknown Role = ""
	// RolePC — машина владельца: интерактивная, полный доступ.
	RolePC Role = "pc"
	// RoleHeadless — фоновая машина: systemd-таймеры, без входа в сессию.
	RoleHeadless Role = "headless"
)

// Valid сообщает, задана ли роль верно.
func (r Role) Valid() bool {
	return r == RolePC || r == RoleHeadless
}

// String возвращает имя роли.
func (r Role) String() string {
	if r == RoleUnknown {
		return "не задан"
	}
	return string(r)
}

// Config — настройки машины.
type Config struct {
	// MachineScope — разделы, которые машина расшифровывает.
	MachineScope []string `yaml:"machine_scope"`
	// Role — профиль машины: pc или headless.
	Role Role `yaml:"role,omitempty"`
	// PullOnly — машина только забирает изменения, ничего не публикует.
	PullOnly bool `yaml:"pull_only,omitempty"`
	// UnlockTTL — срок жизни маркера разблокировки ("24h").
	UnlockTTL string `yaml:"unlock_ttl"`
}

// Path возвращает путь к файлу настроек.
func Path() (string, error) {
	home, err := store.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, FileName), nil
}

// Load читает настройки машины.
//
// Отсутствующий файл — не ошибка: машина без настроек работает, просто
// ничего не расшифровывает, и это видно в doctor.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return Config{}, fmt.Errorf("не удалось разобрать %s: %w", path, err)
	}
	config.MachineScope = normalize(config.MachineScope)
	if config.Role != RoleUnknown && !config.Role.Valid() {
		return Config{}, fmt.Errorf("в %s: неизвестная роль %q (допустимо: %s, %s)",
			path, config.Role, RolePC, RoleHeadless)
	}
	return config, nil
}

// Save записывает настройки машины.
func Save(config Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("не удалось создать каталог %s: %w", filepath.Dir(path), err)
	}
	config.MachineScope = normalize(config.MachineScope)
	content, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("не удалось собрать %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("не удалось записать %s: %w", path, err)
	}
	return nil
}

// Allows сообщает, входит ли раздел в scope машины.
//
// Раздел определяется первым компонентом пути: personal/github — это
// раздел personal.
func (c Config) Allows(section string) bool {
	if section == "" {
		return false
	}
	for _, allowed := range c.MachineScope {
		if allowed == section {
			return true
		}
	}
	return false
}

// TTL возвращает срок жизни маркера разблокировки.
//
// Пустая или битая настройка — дефолт: маркер живёт сутки, чтобы один
// пропущенный тик таймера не закрывал гейт на рабочей машине.
func (c Config) TTL(defaultTTL time.Duration) time.Duration {
	value := strings.TrimSpace(c.UnlockTTL)
	if value == "" {
		return defaultTTL
	}
	ttl, err := time.ParseDuration(value)
	if err != nil || ttl <= 0 {
		return defaultTTL
	}
	return ttl
}

// Section возвращает раздел файла по пути внутри хранилища.
//
// Путь без разделителя относится к корню: его раздел — пустая строка,
// и такой файл ни в один scope не попадает.
func Section(path string) string {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	parts := strings.SplitN(normalized, "/", 2)
	if len(parts) < 2 || parts[0] == "" {
		return ""
	}
	return parts[0]
}

// Detect определяет профиль машины по окружению.
//
// Эвристика: машина без systemd — интерактивная (ПК владельца), машина
// с systemd — фоновая. Явная настройка в файле всегда важнее
// эвристики: владелец знает, чем его машина является.
func Detect() Role {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return RolePC
	}
	return RoleHeadless
}

// normalize убирает пустые значения и дубликаты, сортирует список.
func normalize(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
