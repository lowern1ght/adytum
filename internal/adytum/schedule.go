package adytum

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lowern1ght/adytum/internal/machine"
)

// TimerUnit — имя systemd-таймера автосинхронизации.
const TimerUnit = "adytum-sync.timer"

// TimerService — имя systemd-юнита автосинхронизации.
const TimerService = "adytum-sync.service"

// SyncInterval — период автосинхронизации.
const SyncInterval = "5m"

// ScheduleResult — результат установки таймера.
type ScheduleResult struct {
	// Installed — таймер установлен и включён.
	Installed bool `json:"installed"`
	// Unit — имя таймера.
	Unit string `json:"unit"`
	// Interval — период синхронизации.
	Interval string `json:"interval"`
	// System — таймер установлен на уровне системы (иначе пользователя).
	System bool `json:"system"`
	// Active — таймер активен.
	Active bool `json:"active"`
	// Linger — включён ли linger для пользователя.
	//
	// linger нужен фоновой машине: без него пользовательские юниты
	// не стартуют, пока никто не вошёл в сессию.
	Linger string `json:"linger,omitempty"`
	// Reason — почему таймер не установлен (если не удалось).
	Reason string `json:"reason,omitempty"`
}

// Schedule устанавливает таймер автосинхронизации.
//
// Таймер выполняет только pull: push из таймера никогда не выполняется,
// чтобы фоновая задача не публиковала изменения без участия человека.
//
// На системах без systemd команда завершается понятной ошибкой:
// планировщик там настраивается вручную.
func (a *App) Schedule() (ScheduleResult, error) {
	result := ScheduleResult{
		Unit:     TimerUnit,
		Interval: SyncInterval,
	}
	if runtime.GOOS != "linux" {
		result.Reason = "systemd доступен только на Linux"
		return result, fmt.Errorf("systemd доступен только на Linux: настройте таймер вручную (cron, launchd, Планировщик задач)")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		result.Reason = "systemd не найден"
		return result, fmt.Errorf("systemd не найден: настройте таймер вручную (cron, launchd, Планировщик задач)")
	}

	binary, err := os.Executable()
	if err != nil {
		binary = "adytum"
	}

	unitDir, err := a.systemdUserDir()
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return result, err
	}

	service := a.serviceUnit(binary)
	timer := a.timerUnit()

	if err := os.WriteFile(filepath.Join(unitDir, TimerService), []byte(service), 0o644); err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(unitDir, TimerUnit), []byte(timer), 0o644); err != nil {
		return result, err
	}

	if _, err := a.Runner.Run(Command{}, "systemctl", "--user", "daemon-reload"); err != nil {
		return result, fmt.Errorf("не удалось перечитать юниты systemd: %w", err)
	}
	if _, err := a.Runner.Run(Command{}, "systemctl", "--user", "enable", "--now", TimerUnit); err != nil {
		return result, fmt.Errorf("не удалось включить таймер: %w", err)
	}
	result.Installed = true
	result.Active = true
	result.Linger = a.enableLinger()
	return result, nil
}

// enableLinger включает linger для текущего пользователя.
//
// Фоновая машина живёт без входа в сессию: пользовательские юниты
// systemd без linger не стартуют, и таймер молча не работает. На
// интерактивной машине linger не нужен, поэтому там не включается.
func (a *App) enableLinger() string {
	if a.role() != machine.RoleHeadless {
		return "не требуется"
	}
	user := os.Getenv("USER")
	if user == "" {
		return "не удалось: неизвестен пользователь"
	}
	if _, err := a.Runner.Run(Command{}, "loginctl", "enable-linger", user); err != nil {
		return "не удалось: " + err.Error()
	}
	return "включён"
}

// systemdUserDir возвращает каталог пользовательских юнитов systemd.
func (a *App) systemdUserDir() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user"), nil
}

// serviceUnit возвращает текст systemd-юнита.
//
// ExecStart выполняет только pull: изменения из таймера не уходят на
// remote.
//
// Environment=HOME обязателен: без него systemd запускает юнит с
// пустым HOME, и CLI не находит ни ~/.adytum, ни ключ машины.
//
// WorkingDirectory указывает на хранилище: иначе git ищет репозиторий
// от корня файловой системы и падает.
//
// After=network-online.target ждёт сеть: без сети первый запуск
// таймера падает, и systemd откладывает его на сутки.
func (a *App) serviceUnit(binary string) string {
	return fmt.Sprintf(`[Unit]
Description=adytum: автосинхронизация секретов (только pull)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
Environment=HOME=%s
WorkingDirectory=%s
ExecStart=%s sync --pull-only
`, a.homeDir(), a.Root, binary)
}

// timerUnit возвращает текст systemd-таймера.
func (a *App) timerUnit() string {
	return fmt.Sprintf(`[Unit]
Description=adytum: таймер автосинхронизации секретов
After=network-online.target
Wants=network-online.target

[Timer]
OnBootSec=%s
OnUnitActiveSec=%s
Persistent=true

[Install]
WantedBy=timers.target
`, SyncInterval, SyncInterval)
}

// homeDir возвращает домашний каталог машины.
//
// Значение подставляется в Environment= юнита: systemd не знает, где
// HOME пользователя, а без него CLI не найдёт состояние машины.
func (a *App) homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/root"
	}
	return strings.ReplaceAll(home, "\\", "/")
}

// ScheduleStatus возвращает текущее состояние таймера.
func (a *App) ScheduleStatus() ScheduleResult {
	result := ScheduleResult{Unit: TimerUnit, Interval: SyncInterval}
	if runtime.GOOS != "linux" {
		result.Reason = "systemd доступен только на Linux"
		return result
	}
	out, err := a.Runner.Run(Command{}, "systemctl", "--user", "is-active", TimerUnit)
	if err != nil {
		result.Reason = "таймер не найден"
		return result
	}
	result.Active = strings.TrimSpace(string(out)) == "active"
	result.Installed = true
	return result
}
