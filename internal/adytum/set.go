package adytum

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/secretvalue"
	"github.com/lowern1ght/adytum/internal/ui"
	"golang.org/x/term"
)

// SetOptions — параметры команды set.
type SetOptions struct {
	// Key — ключ, который задаётся. Пустой — заносится весь набор
	// ключей из реестра.
	Key string
	// FromStdin — значение читается из stdin.
	FromStdin bool
}

// Set заносит значения в секрет.
//
// Значения вводит владелец: либо скрытым вводом, либо через stdin.
// Значение в argv не принимается никогда — оно останется в списке
// процессов и в истории оболочки.
//
// Запись должна быть в реестре: без описания в реестре агент не знает,
// что и куда занести. Создаётся она командой `adytum new`.
func (a *App) Set(entry string, options SetOptions) error {
	if entry == "" {
		return fmt.Errorf("%w: укажите секрет: adytum set <project>/<service>", ErrUsage)
	}
	if err := a.requireUnlock(); err != nil {
		return err
	}
	if options.FromStdin && options.Key == "" {
		return fmt.Errorf("%w: из stdin задаётся один ключ: adytum set <project>/<service> <KEY> --stdin", ErrUsage)
	}

	loaded, err := a.loadRegistry()
	if err != nil {
		if !errors.Is(err, ErrRegistryMissing) {
			return err
		}
		// Реестра нет — запись создаётся вместе с секретом.
		loaded = registry.New()
	}

	record, found := loaded.Entry(entry)
	if !found {
		return a.setWithNewEntry(entry, options)
	}
	if !record.Type.Keyed() {
		return advise(
			fmt.Errorf("секрет типа %s не имеет ключей", record.Type),
			ui.KindArrow,
			fmt.Sprintf("правьте его редактором: adytum edit %s", entry),
		)
	}

	path, err := a.secretPath(entry, record.Type)
	if err != nil {
		return err
	}
	if err := a.requireInScope(path); err != nil {
		return err
	}

	if options.Key != "" {
		return a.setOneKey(path, record, options)
	}
	return a.setAllKeys(path, record, options)
}

// setOneKey заносит или обновляет один ключ.
func (a *App) setOneKey(path string, record registry.Entry, options SetOptions) error {
	value, err := a.readValue(record, options.Key, options.FromStdin)
	if err != nil {
		return err
	}
	if err := a.writeKey(path, record.Type, options.Key, value); err != nil {
		return err
	}
	a.printf("ключ %s обновлён в %s\n", options.Key, relativeTo(a.Root, path))
	return nil
}

// setAllKeys заносит весь набор ключей записи реестра.
//
// Набор ключей берётся из реестра: там описано, что секрету полагается
// иметь, и какие ключи уже заполнены.
func (a *App) setAllKeys(path string, record registry.Entry, options SetOptions) error {
	if len(record.Keys) == 0 {
		return fmt.Errorf("%w: в реестре нет ключей для %s", ErrUsage, record.Name)
	}

	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	updated := content
	for _, key := range sortedMapKeys(record.Keys) {
		description := record.Keys[key]
		filled := a.keyFilled(record.Type, content, key)
		if filled {
			a.printf("%s (%s): [заполнено, Enter — оставить] ", key, description)
			keep, err := a.confirmKeep()
			if err != nil {
				return err
			}
			if keep {
				continue
			}
		}
		value, err := a.readValue(record, key, options.FromStdin)
		if err != nil {
			return err
		}
		updated, err = secretvalue.Set(record.Type.Format(), updated, key, value)
		if err != nil {
			return err
		}
	}
	if err := a.requireEncryptedPath(relativeTo(a.Root, path)); err != nil {
		return err
	}

	if err := a.writePlaintext(path, updated); err != nil {
		return err
	}
	if err := a.stageFile(path); err != nil {
		return err
	}
	a.printf("обновлён %s\n", relativeTo(a.Root, path))
	return nil
}

// setWithNewEntry создаёт секрет вместе с записью в реестре.
//
// Секрета нет и описания тоже: владелец сначала отвечает на
// вопросы мастера, потом вводит значения.
func (a *App) setWithNewEntry(entry string, options SetOptions) error {
	if options.Key == "" {
		return fmt.Errorf("%w: секрет %s не описан в реестре: создайте его: adytum new %s", ErrUsage, entry, entry)
	}
	return advise(
		fmt.Errorf("%w: секрет %s не описан в реестре", ErrNotFound, entry),
		ui.KindArrow,
		fmt.Sprintf("создайте запись: adytum new %s", entry),
	)
}

// keyFilled сообщает, заполнен ли ключ в файле.
func (a *App) keyFilled(format registry.Type, content []byte, key string) bool {
	value, found, err := secretvalue.Get(format.Format(), content, key)
	if err != nil || !found {
		return false
	}
	return value != ""
}

// readValue читает значение ключа.
//
// Значение приходит из скрытого ввода или из stdin. Источник выбирает
// владелец, но не агент: значение не должно попасть в argv.
func (a *App) readValue(record registry.Entry, key string, fromStdin bool) (string, error) {
	if fromStdin {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("не удалось прочитать stdin: %w", err)
		}
		return strings.TrimRight(string(content), "\r\n"), nil
	}
	description := record.Keys[key]
	return a.readSecret(description)
}

// readSecret читает значение скрытым вводом.
//
// Значение не отображается на экране: иначе оно попадёт в снимок
// экрана и в запись сессии.
func (a *App) readSecret(description string) (string, error) {
	if description == "" {
		description = "значение"
	}
	a.printf("%s: ", description)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	a.println("")
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать значение: %w", err)
	}
	secret := string(value)
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("%w: значение не может быть пустым", ErrUsage)
	}
	return secret, nil
}

// confirmKeep спрашивает, оставить ли прежнее значение.
//
// Пустой ответ означает «оставить»: по умолчанию секрет не
// затирается.
func (a *App) confirmKeep() (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		return false, fmt.Errorf("%w: не удалось прочитать ответ: %v", ErrUsage, err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes" || answer == "д", nil
}

// writeKey записывает ключ в файл секрета.
func (a *App) writeKey(path string, format registry.Type, key, value string) error {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated, err := secretvalue.Set(format.Format(), content, key, value)
	if err != nil {
		return err
	}
	if err := a.writePlaintext(path, updated); err != nil {
		return err
	}
	return a.stageFile(path)
}

// Unset удаляет ключ из секрета.
func (a *App) Unset(entry, key string) error {
	if entry == "" || key == "" {
		return fmt.Errorf("%w: укажите секрет и ключ: adytum unset <project>/<service> <KEY>", ErrUsage)
	}
	if err := a.requireUnlock(); err != nil {
		return err
	}
	loaded, err := a.loadRegistry()
	if err != nil {
		return a.registryError(err)
	}
	record, found := loaded.Entry(entry)
	if !found {
		return fmt.Errorf("%w: запись %s не найдена в реестре", ErrNotFound, entry)
	}
	path, err := a.secretPath(entry, record.Type)
	if err != nil {
		return err
	}
	if err := a.requireInScope(path); err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := secretvalue.Unset(record.Type.Format(), content, key)
	if err != nil {
		return err
	}
	if err := a.requireEncryptedPath(relativeTo(a.Root, path)); err != nil {
		return err
	}

	if err := a.writePlaintext(path, updated); err != nil {
		return err
	}
	if err := a.stageFile(path); err != nil {
		return err
	}
	a.printf("ключ %s удалён из %s\n", key, relativeTo(a.Root, path))
	return nil
}

// secretPath возвращает путь файла секрета по записи реестра.
//
// Расширение берётся из типа: тип объявляет формат файла, и путать
// его с расширением нельзя.
func (a *App) secretPath(entry string, secretType registry.Type) (string, error) {
	extension := secretType.Extension()
	if extension == "" {
		extension = ".env"
	}
	path := filepath.Join(a.Root, filepath.FromSlash(entry)+extension)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	// Файла нет: ищем существующий с другим расширением, чтобы
	// обновить именно его, а не создать второй файл рядом.
	for _, candidate := range []string{".env", ".yaml", ".yml", ".json"} {
		existing := filepath.Join(a.Root, filepath.FromSlash(entry)+candidate)
		if _, err := os.Stat(existing); err == nil {
			return existing, nil
		}
	}
	return path, nil
}

// missingSecretError собирает ошибку об отсутствующем секрете.
//
// Ошибка адресована агенту: в ней готовая команда для владельца и
// описание ключа из реестра. Значение при этом не требуется и не
// запрашивается.
func (a *App) missingSecretError(entry, key string) error {
	description := ""
	command := fmt.Sprintf("adytum new %s", entry)

	loaded, err := a.loadRegistry()
	if err == nil {
		if record, found := loaded.Entry(entry); found {
			if key != "" {
				if text, ok := record.Keys[key]; ok {
					description = text
				}
				command = fmt.Sprintf("adytum set %s %s", entry, key)
			} else {
				command = fmt.Sprintf("adytum set %s", entry)
			}
		}
	}

	message := fmt.Sprintf("секрета нет: %s", entry)
	if key != "" {
		message = fmt.Sprintf("секрета нет: %s (%s)", entry, key)
	}
	if description != "" {
		message = fmt.Sprintf("%s — %s", message, description)
	}
	return advise(
		fmt.Errorf("%s", message),
		ui.KindWarn,
		"владелец: "+command,
	)
}

// missingKeyError собирает ошибку об отсутствующем ключе.
//
// Ключа нет, а секрет есть: значит, имя ключа в запросе не совпадает с
// тем, что лежит в файле. Состав ключей показывает реестр — поэтому
// подсказка ведёт к `adytum list`, а не к владельцу.
func (a *App) missingKeyError(entry, key string) error {
	description := ""
	loaded, err := a.loadRegistry()
	if err == nil {
		if record, found := loaded.Entry(entry); found {
			description = record.Keys[key]
		}
	}

	message := fmt.Sprintf("ключа нет: %s в %s", key, entry)
	if description != "" {
		message = fmt.Sprintf("%s — %s", message, description)
	}
	return advise(
		fmt.Errorf("%s", message),
		ui.KindWarn,
		fmt.Sprintf("состав секрета: adytum list %s", entry),
	)
}
