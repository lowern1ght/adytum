package adytum

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	return result, nil
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
func (a *App) serviceUnit(binary string) string {
	return fmt.Sprintf(`[Unit]
Description=adytum: автосинхронизация секретов (только pull)

[Service]
Type=oneshot
WorkingDirectory=%s
ExecStart=%s sync --pull-only
`, a.Root, binary)
}

// timerUnit возвращает текст systemd-таймера.
func (a *App) timerUnit() string {
	return fmt.Sprintf(`[Unit]
Description=adytum: таймер автосинхронизации секретов

[Timer]
OnBootSec=%s
OnUnitActiveSec=%s
Persistent=true

[Install]
WantedBy=timers.target
`, SyncInterval, SyncInterval)
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
