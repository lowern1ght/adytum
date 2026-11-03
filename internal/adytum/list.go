package adytum

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/store"
	"github.com/lowern1ght/adytum/internal/ui"
)

// RegistryPath возвращает путь к реестру секретов.
func (a *App) RegistryPath() string {
	return filepath.Join(a.Root, registry.FileName)
}

// ErrRegistryMissing — файл реестра отсутствует.
//
// Отсутствие реестра — не поломка: хранилище может существовать и без
// него. Команды, которым реестр нужен, сообщают, что его надо создать.
var ErrRegistryMissing = errors.New("реестр не найден")

// loadRegistry читает реестр секретов.
//
// Отсутствие реестра — не ошибка разбора: возвращается ErrRegistryMissing,
// и вызывающий сам решает, что с этим делать — создать реестр или
// сообщить о нём.
func (a *App) loadRegistry() (*registry.Registry, error) {
	content, err := os.ReadFile(a.RegistryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrRegistryMissing, a.RegistryPath())
		}
		return nil, fmt.Errorf("не удалось прочитать реестр: %w", err)
	}
	return registry.Parse(content)
}

// registryError превращает ошибку чтения реестра в понятную подсказку.
//
// Отсутствующий реестр — обычное состояние первого запуска: команда
// должна сказать, что создать, а не молча упасть.
func (a *App) registryError(err error) error {
	if errors.Is(err, ErrRegistryMissing) {
		return advise(
			fmt.Errorf("реестра нет: %s", a.RegistryPath()),
			ui.KindWarn,
			"создайте реестр: adytum new <project>/<service>",
		)
	}
	return err
}

// localRecipients читает локальный реестр получателей.
//
// Файл лежит в ~/.adytum/recipients и заполняется командой grant.
func (a *App) localRecipients() map[string]string {
	recipients, err := store.LoadRecipients()
	if err != nil {
		return map[string]string{}
	}
	return recipients
}

// ListEntry — строка реестра для таблицы.
type ListEntry struct {
	// Name — путь секрета в хранилище.
	Name string `json:"name"`
	// Type — тип секрета.
	Type string `json:"type"`
	// Description — короткое описание.
	Description string `json:"description"`
	// Scope — scope, определяющий получателей.
	Scope string `json:"scope"`
	// Recipients — машины, которым виден раздел.
	Recipients []string `json:"recipients"`
	// Stale — секрет устарел относительно remote.
	Stale bool `json:"stale"`
	// Staleness — состояние свежести.
	Staleness string `json:"staleness"`
}

// ListReport — результат команды list.
type ListReport struct {
	// Entries — записи реестра.
	Entries []ListEntry `json:"entries"`
	// Errors — проблемы реестра, если они есть.
	Errors []string `json:"errors,omitempty"`
}

// List печатает реестр секретов.
//
// Без аргумента показывает все записи, с аргументом — подробно одну.
func (a *App) List(name string) error {
	loaded, err := a.loadRegistry()
	if err != nil {
		if errors.Is(err, ErrRegistryMissing) {
			return a.listWithoutRegistry()
		}
		return err
	}

	if name != "" {
		return a.listDetail(loaded, name)
	}
	return a.listTable(loaded)
}

// listWithoutRegistry сообщает об отсутствии реестра.
//
// Отсутствие реестра — не поломка: хранилище может быть пустым или ещё
// не переведено на реестр. Команда говорит, что создать, и завершается
// без ошибки: падать на пустом хранилище нечем.
func (a *App) listWithoutRegistry() error {
	notice := "реестра нет: " + a.RegistryPath()

	if a.JSON {
		encoded, err := json.MarshalIndent(ListReport{
			Entries: []ListEntry{},
			Errors:  []string{notice},
		}, "", "  ")
		if err != nil {
			return err
		}
		a.println(string(encoded))
		return nil
	}

	printer := a.uiErr()
	printer.Warn("%s", notice)
	printer.Hint("создайте реестр: adytum new <project>/<service>")
	return nil
}

// listTable печатает таблицу всех записей реестра.
func (a *App) listTable(loaded *registry.Registry) error {
	report := a.listReport(loaded)

	if a.JSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		a.println(string(encoded))
		return nil
	}

	printer := a.ui()
	if len(report.Entries) == 0 {
		printer.Plain("реестр пуст")
	}
	for _, entry := range report.Entries {
		printer.Plain("%-28s %-11s %-9s %-11s %s",
			entry.Name, entry.Type, entry.Scope, entry.Staleness, entry.Description)
	}
	for _, problem := range report.Errors {
		printer.Fail("%s", problem)
	}
	if len(report.Errors) > 0 {
		printer.Hint("исправьте registry.yaml; доступ выдаётся: adytum grant <label> --scope <scope>")
	}
	return nil
}

// listDetail печатает подробности одной записи реестра.
//
// Значения секретов не показываются: описания ключей и адрес ротации —
// да, содержимое — нет.
func (a *App) listDetail(loaded *registry.Registry, name string) error {
	entry, found := loaded.Entry(name)
	if !found {
		return fmt.Errorf("%w: запись %s не найдена в реестре", ErrNotFound, name)
	}

	recipients := []string{}
	if definition, ok := loaded.Scope(entry.Scope); ok {
		recipients = definition.Machines
	}

	if a.JSON {
		payload := map[string]any{
			"name":        entry.Name,
			"type":        string(entry.Type),
			"description": entry.Description,
			"scope":       entry.Scope,
			"recipients":  recipients,
			"keys":        entry.Keys,
		}
		if entry.Rotates != "" {
			payload["rotates"] = entry.Rotates
		}
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		a.println(string(encoded))
		return nil
	}

	printer := a.ui()
	printer.Plain("запись:         %s", entry.Name)
	printer.Plain("type:           %s", entry.Type)
	printer.Plain("описание:       %s", entry.Description)
	printer.Plain("scope:          %s", entry.Scope)
	if len(recipients) > 0 {
		printer.Plain("расшифровывают: %s", strings.Join(recipients, ", "))
	} else {
		printer.Fail("scope %s не определён в реестре: расшифровать некому", entry.Scope)
	}
	if entry.Rotates != "" {
		printer.Plain("ротация:        %s", entry.Rotates)
	}
	if len(entry.Keys) > 0 {
		printer.Plain("ключи:")
		for _, key := range sortedMapKeys(entry.Keys) {
			printer.Plain("  %s — %s", key, entry.Keys[key])
		}
	}
	return nil
}

// listReport собирает отчёт по реестру.
//
// Свежесть одна для всего хранилища: remote отстаёт целиком, а не по
// отдельным секретам.
func (a *App) listReport(loaded *registry.Registry) ListReport {
	report := ListReport{Errors: loaded.Errors}

	stale, behind := a.stalenessState()

	for _, name := range loaded.EntryNames() {
		entry := loaded.Entries[name]
		recipients := []string{}
		if definition, ok := loaded.Scope(entry.Scope); ok {
			recipients = definition.Machines
		}
		report.Entries = append(report.Entries, ListEntry{
			Name:        entry.Name,
			Type:        string(entry.Type),
			Description: entry.Description,
			Scope:       entry.Scope,
			Recipients:  recipients,
			Stale:       stale,
			Staleness:   stalenessLabel(stale, behind),
		})
	}
	return report
}

// stalenessState определяет состояние свежести хранилища.
func (a *App) stalenessState() (bool, int) {
	result := a.stalenessChecker().Check()
	if result.Err != nil {
		return false, 0
	}
	return result.Stale(), result.Behind
}

// stalenessLabel возвращает подпись состояния свежести.
func stalenessLabel(stale bool, behind int) string {
	if stale {
		return fmt.Sprintf("устарел (+%d)", behind)
	}
	return "актуально"
}

// sortedMapKeys возвращает имена ключей карты в алфавитном порядке.
func sortedMapKeys(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
