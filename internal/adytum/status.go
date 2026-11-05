package adytum

import (
	"encoding/json"
	"os"

	"github.com/lowern1ght/adytum/internal/sopsconf"
	"github.com/lowern1ght/adytum/internal/store"
)

// StatusReport — состояние репозитория в машинночитаемом виде.
type StatusReport struct {
	// Branch — текущая ветка.
	Branch string `json:"branch"`
	// Remote — адрес remote origin.
	Remote string `json:"remote"`
	// Recipients — публичные ключи получателей из .sops.yaml.
	Recipients []string `json:"recipients"`
	// Unlock — состояние маркера разблокировки.
	Unlock UnlockReport `json:"unlock"`
	// Locked — закрыт ли доступ к операциям get и run.
	Locked bool `json:"locked"`
	// Reason — причина закрытия: locked (маркера нет или истёк)
	// или broken (маркер есть, но нечитаем).
	Reason string `json:"reason,omitempty"`
	// Filters — настроенные git-фильтры adytum.
	Filters map[string]string `json:"filters"`
}

// UnlockReport — состояние маркера разблокировки.
type UnlockReport struct {
	// Active — маркер действует.
	Active bool `json:"active"`
	// ExpiresAt — момент истечения маркера.
	ExpiresAt string `json:"expires_at,omitempty"`
	// TTL — срок жизни маркера ("24h0m0s").
	TTL string `json:"ttl,omitempty"`
}

// Status печатает состояние репозитория.
//
// Показывает ветку, remote, получателей из .sops.yaml, состояние
// маркера разблокировки и наличие git-фильтров.
func (a *App) Status() error {
	report := a.statusReport()
	printer := a.ui()
	printer.Plain("ветка:    %s", orUnknown(report.Branch))
	printer.Plain("remote:   %s", orUnknown(report.Remote))
	if len(report.Recipients) == 0 {
		printer.Plain("получатели: не заданы")
	}
	for _, recipient := range report.Recipients {
		printer.Plain("получатель: %s", recipient)
	}
	if report.Unlock.Active {
		printer.Plain("разблокировка: активна до %s", report.Unlock.ExpiresAt)
	} else {
		printer.Plain("разблокировка: не активна")
		printer.Hint("%s", unlockAdvice(a.role()))
	}
	for _, key := range []string{"filter.adytum.clean", "filter.adytum.smudge", "filter.adytum.required"} {
		value, ok := report.Filters[key]
		if !ok || value == "" {
			printer.Plain("git %s: не настроен", key)
			continue
		}
		printer.Plain("git %s: %s", key, value)
	}
	a.staleHint(a.out())
	return nil
}

// StatusJSON печатает состояние репозитория в машинночитаемом виде.
func (a *App) StatusJSON() error {
	encoded, err := json.MarshalIndent(a.statusReport(), "", "  ")
	if err != nil {
		return err
	}
	a.println(string(encoded))
	return nil
}

// statusReport собирает состояние репозитория.
func (a *App) statusReport() StatusReport {
	branch, _ := a.gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	remote, _ := a.gitOutput("remote", "get-url", "origin")

	report := StatusReport{
		Branch:  branch,
		Remote:  remote,
		Filters: map[string]string{},
	}

	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err == nil {
		if config, parseErr := sopsconf.Parse(content); parseErr == nil {
			report.Recipients = config.Recipients()
		}
	}

	state, err := store.Unlock(a.Now(), a.unlockTTL())
	report.Unlock.TTL = a.unlockTTL().String()
	if err == nil && state.Unlocked {
		report.Unlock.Active = true
		report.Unlock.ExpiresAt = state.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
	} else if err == nil && state.Broken {
		report.Reason = "broken"
	} else if err == nil {
		report.Reason = "locked"
	}
	report.Locked = !report.Unlock.Active

	for _, key := range []string{"filter.adytum.clean", "filter.adytum.smudge", "filter.adytum.required"} {
		value, err := a.gitOutput("config", "--get", key)
		if err != nil {
			continue
		}
		report.Filters[key] = value
	}
	return report
}
