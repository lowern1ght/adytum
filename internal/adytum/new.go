package adytum

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lowern1ght/adytum/internal/registry"
)

// NewOptions — параметры создания секрета.
type NewOptions struct {
	// Type — тип секрета.
	Type registry.Type
	// Description — человеческое описание.
	Description string
	// Scope — scope, определяющий получателей.
	Scope string
	// DescriptionSet — описание введено пользователем.
	DescriptionSet bool
}

// New создаёт новый секрет: файл-шаблон, запись в реестре и коммит.
//
// Описание и scope запрашиваются у пользователя, если они не заданы:
// реестр без описания нечем читать, а scope без получателей не даёт
// доступа никому.
//
// Реестра может не быть: тогда он создаётся из шаблона, иначе первый
// секрет негде описать.
func (a *App) New(entry string, options NewOptions) error {
	if entry == "" {
		return fmt.Errorf("%w: укажите проект и сервис: adytum new <project>/<service>", ErrUsage)
	}
	project, service, err := SplitTarget(entry)
	if err != nil {
		return err
	}
	entry = project + "/" + service

	loaded, err := a.loadRegistry()
	if err != nil {
		if !errors.Is(err, ErrRegistryMissing) {
			return err
		}
		if err := a.ensureRegistryFile(); err != nil {
			return err
		}
		loaded, err = a.loadRegistry()
		if err != nil {
			return err
		}
	}
	if loaded.HasEntry(entry) {
		return fmt.Errorf("запись %s уже есть в реестре", entry)
	}

	secretType := options.Type
	if secretType == "" {
		secretType, err = a.askType()
		if err != nil {
			return err
		}
	}
	if !secretType.Valid() {
		return fmt.Errorf("%w: неизвестный type %q (допустимо: %s)", ErrUsage, secretType, typeChoices())
	}

	description := options.Description
	if !options.DescriptionSet || description == "" {
		description, err = a.askDescription()
		if err != nil {
			return err
		}
	}

	scopeName := options.Scope
	if scopeName == "" {
		scopeName, err = a.askScope(loaded)
		if err != nil {
			return err
		}
	}
	if _, found := loaded.Scope(scopeName); !found {
		return fmt.Errorf("%w: scope %s не определён в реестре", ErrUsage, scopeName)
	}

	path := filepath.Join(a.Root, filepath.FromSlash(entry)+secretType.Extension())
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("секрет %s уже существует", relativeTo(a.Root, path))
	}
	// Файл должен попасть под шифрование: без правила в
	// .gitattributes он уедет в remote открытым.
	if err := a.requireEncryptedPath(relativeTo(a.Root, path)); err != nil {
		return err
	}
	if err := a.writePlaintext(path, []byte(secretTemplate(secretType))); err != nil {
		return err
	}
	if err := a.stageFile(path); err != nil {
		return err
	}

	if err := a.updateRegistry(func(document *registry.Document) error {
		return document.SetEntry(registry.Entry{
			Name:        entry,
			Type:        secretType,
			Description: description,
			Scope:       scopeName,
		})
	}); err != nil {
		return err
	}
	if err := a.stageFile(a.RegistryPath()); err != nil {
		return err
	}

	message := fmt.Sprintf("[.stbl](feat/adytum): add %s secret entry", entry)
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("не удалось закоммитить: %w", err)
	}
	a.printf("создан %s\n", relativeTo(a.Root, path))
	a.printf("запись в реестре: %s (scope %s)\n", entry, scopeName)
	if definition, found := loaded.Scope(scopeName); found && len(definition.Machines) == 0 {
		a.hint("в scope %s машин нет — выдайте доступ: adytum grant <label> --scope %s", scopeName, scopeName)
	}
	a.hint("заполните секрет: adytum set %s", entry)
	return nil
}

// askType спрашивает тип секрета.
func (a *App) askType() (registry.Type, error) {
	printer := a.ui()
	for index, secretType := range registry.Types() {
		printer.Plain("%d) %s", index+1, secretType)
	}
	answer, err := a.ask("тип секрета")
	if err != nil {
		return "", err
	}
	index, err := strconv.Atoi(answer)
	if err != nil || index < 1 || index > len(registry.Types()) {
		return "", fmt.Errorf("%w: ожидался номер от 1 до %d", ErrUsage, len(registry.Types()))
	}
	return registry.Types()[index-1], nil
}

// askDescription спрашивает описание секрета.
func (a *App) askDescription() (string, error) {
	answer, err := a.ask("описание")
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("%w: описание не может быть пустым", ErrUsage)
	}
	return answer, nil
}

// askScope спрашивает scope из списка определённых в реестре.
func (a *App) askScope(loaded *registry.Registry) (string, error) {
	printer := a.ui()
	names := loaded.ScopeNames()
	for index, name := range names {
		printer.Plain("%d) %s — %s", index+1, name, strings.Join(loaded.Scopes[name].Machines, ", "))
	}
	answer, err := a.ask("scope")
	if err != nil {
		return "", err
	}
	index, err := strconv.Atoi(answer)
	if err != nil || index < 1 || index > len(names) {
		return "", fmt.Errorf("%w: ожидался номер от 1 до %d", ErrUsage, len(names))
	}
	return names[index-1], nil
}

// ask задаёт вопрос и читает ответ.
//
// Ввод идёт из stdin: мастер должен работать и без терминала.
func (a *App) ask(question string) (string, error) {
	a.printf("%s: ", question)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return "", fmt.Errorf("%w: не удалось прочитать ответ: %v", ErrUsage, err)
	}
	return strings.TrimSpace(answer), nil
}

// typeChoices возвращает список типов для подсказки.
func typeChoices() string {
	names := make([]string, 0, len(registry.Types()))
	for _, secretType := range registry.Types() {
		names = append(names, string(secretType))
	}
	return strings.Join(names, "|")
}

// secretTemplate возвращает заготовку файла для типа.
//
// Заготовка содержит только подсказки: значения в ней не должно быть,
// иначе агент закоммитит мусор.
func secretTemplate(secretType registry.Type) string {
	switch secretType {
	case registry.TypeEnv:
		return "# example KEY=\n"
	case registry.TypeYAML:
		return "# example_key: example-value\n"
	case registry.TypeJSON:
		return "{\n  \"example_key\": \"example-value\"\n}\n"
	default:
		return "# example_key: example-value\n"
	}
}
