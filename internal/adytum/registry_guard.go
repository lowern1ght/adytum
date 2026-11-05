package adytum

import (
	"fmt"
	"strings"

	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/ui"
)

// registryProblems собирает проблемы реестра, из-за которых правила
// шифрования собираются неверно.
//
// Три проблемы опаснее остальных:
//
//   - scope, которого нет в реестре: запись под него попала бы под
//     правило по умолчанию (^.*$) и получила чужих получателей;
//   - scope без машин, на который ссылаются записи: файлы этих
//     записей уехали бы под то же правило по умолчанию;
//   - ошибки разбора: часть записей вообще не разобралась.
//
// Scope без машин, на который записи не ссылаются, проблемой не
// считается: он пуст намеренно, и правило для него не нужно.
//
// except — scope, который команда сейчас чинит. Его проблемы
// игнорируются: выдача доступа в пустой scope как раз и делает его
// непустым.
func registryProblems(loaded *registry.Registry, except string) []string {
	var problems []string
	for _, scope := range loaded.UnknownScopes() {
		problems = append(problems, fmt.Sprintf("scope %s не определён в реестре", scope))
	}
	for _, name := range loaded.ScopeNames() {
		if name == except || !loaded.ScopeHasEntries(name) {
			continue
		}
		if len(loaded.Scopes[name].Machines) > 0 {
			continue
		}
		problems = append(problems, fmt.Sprintf("scope %s пуст, entries не определены", name))
	}
	problems = append(problems, loaded.Errors...)
	return problems
}

// registryHints собирает подсказки к проблемам реестра.
//
// Подсказка говорит, что делать: добавить недостающий scope или
// выдать доступ пустому.
func registryHints(loaded *registry.Registry, except string) []string {
	var hints []string
	if len(loaded.UnknownScopes()) > 0 {
		hints = append(hints, "добавь scope в registry.yaml или исправь entry")
	}
	if len(loaded.Errors) > 0 {
		hints = append(hints, "исправь registry.yaml")
	}
	for _, name := range loaded.ScopeNames() {
		if name == except || !loaded.ScopeHasEntries(name) {
			continue
		}
		if len(loaded.Scopes[name].Machines) > 0 {
			continue
		}
		hints = append(hints, fmt.Sprintf("выдайте доступ: adytum grant <label> --scope %s", name))
	}
	return hints
}

// requireRegistryConsistent проверяет, что реестр пригоден для
// сборки правил шифрования.
//
// Отказ здесь лучше тихой эскалации доступа: файл, ушедший под
// правило по умолчанию, получает получателей чужого раздела.
func requireRegistryConsistent(loaded *registry.Registry, except string) error {
	problems := registryProblems(loaded, except)
	if len(problems) == 0 {
		return nil
	}
	hints := registryHints(loaded, except)
	if len(hints) == 0 {
		hints = []string{"исправь registry.yaml"}
	}
	return advise(
		fmt.Errorf("в реестре: %s", strings.Join(problems, "; ")),
		ui.KindWarn,
		strings.Join(hints, "; "),
	)
}

// checkRevokeScope проверяет, что отзыв не оставит scope без
// получателей.
//
// Пустой scope с записями опасен: файлы этих записей уедут под
// правило по умолчанию и получат чужих получателей. Отказ безопаснее
// тихой эскалации — сначала выдать доступ другой машине.
func (a *App) checkRevokeScope(label string) error {
	loaded, err := a.loadRegistry()
	if err != nil {
		return a.registryError(err)
	}
	emptied := scopesEmptiedByRevoke(loaded, label)
	if len(emptied) == 0 {
		return nil
	}
	return advise(
		fmt.Errorf("scope %s пуст, entries не определены", strings.Join(emptied, ", ")),
		ui.KindWarn,
		fmt.Sprintf("выдайте доступ другой машине: adytum grant <label> --scope %s", emptied[0]),
	)
}

// scopesEmptiedByRevoke возвращает scopes, которые останутся без
// машин после отзыва метки.
func scopesEmptiedByRevoke(loaded *registry.Registry, label string) []string {
	var emptied []string
	for _, name := range loaded.ScopeNames() {
		if !loaded.ScopeHasEntries(name) {
			continue
		}
		remaining := 0
		for _, machine := range loaded.Scopes[name].Machines {
			if machine != label {
				remaining++
			}
		}
		if remaining == 0 {
			emptied = append(emptied, name)
		}
	}
	return emptied
}
