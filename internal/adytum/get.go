package adytum

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/envfile"
	"github.com/lowern1ght/adytum/internal/machine"
	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/secretvalue"
	"github.com/lowern1ght/adytum/internal/ui"
)

// marshalJSON сериализует значение в отформатированный JSON.
func marshalJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}

// Get печатает расшифрованное содержимое секрета.
//
// Если задан key, печатается только значение этого ключа. Значение
// уходит только в stdout.
//
// Операция требует активного маркера разблокировки: без него агентская
// сессия не должна видеть значения.
func (a *App) Get(project, service, key string) error {
	if err := a.requireUnlock(); err != nil {
		return err
	}
	path, _, err := ResolveSecret(a.Root, project, service)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return a.missingSecretError(project+"/"+service, key)
		}
		return err
	}
	if err := a.requireInScope(path); err != nil {
		return err
	}
	plain, err := a.readPlaintext(path)
	if err != nil {
		return err
	}
	if key == "" {
		a.print(string(plain))
	} else {
		value, err := a.secretValue(path, plain, key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return a.missingKeyError(project+"/"+service, key)
			}
			return err
		}
		a.println(value)
	}
	a.staleHint(a.errOut())
	return nil
}

// GetJSON печатает секрет в машинночитаемом виде.
//
// Значение попадает в поле "value" — это ожидаемое поведение команды
// get: вывод запрашивается явно.
func (a *App) GetJSON(project, service, key string) error {
	if err := a.requireUnlock(); err != nil {
		return err
	}
	path, _, err := ResolveSecret(a.Root, project, service)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return a.missingSecretError(project+"/"+service, key)
		}
		return err
	}
	if err := a.requireInScope(path); err != nil {
		return err
	}
	plain, err := a.readPlaintext(path)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"project": project,
		"service": service,
	}
	if key == "" {
		payload["content"] = string(plain)
	} else {
		value, err := a.secretValue(path, plain, key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return a.missingKeyError(project+"/"+service, key)
			}
			return err
		}
		payload["key"] = key
		payload["value"] = value
	}
	encoded, err := marshalJSON(payload)
	if err != nil {
		return err
	}
	a.println(string(encoded))
	return nil
}

// secretValue достаёт значение ключа из секрета.
//
// Способ зависит от типа: env разбирается dotenv-парсером, yaml и json
// — по ключам верхнего уровня. У бинарных типов ключей нет вовсе:
// значение такого секрета — весь файл.
func (a *App) secretValue(path string, plain []byte, key string) (string, error) {
	secretType, err := a.secretType(path)
	if err != nil {
		return "", err
	}
	if !secretType.Keyed() {
		return "", advise(
			fmt.Errorf("секрет типа %s не имеет ключей", secretType),
			ui.KindArrow,
			fmt.Sprintf("читайте файл целиком: adytum get %s", relativeTo(a.Root, path)),
		)
	}
	switch secretType {
	case registry.TypeEnv:
		entries, err := envfile.Parse(plain)
		if err != nil {
			return "", err
		}
		value, found := envfile.Lookup(entries, key)
		if !found {
			return "", fmt.Errorf("%w: %s в %s", ErrKeyNotFound, key, filepath.Base(path))
		}
		return value, nil
	default:
		value, found, err := secretvalue.Get(secretType.Format(), plain, key)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf("%w: %s в %s", ErrKeyNotFound, key, filepath.Base(path))
		}
		return value, nil
	}
}

// secretType определяет тип секрета.
//
// Тип берётся из реестра: он и есть объявление формата файла. Без
// записи в реестре тип определяется по расширению — иначе старые
// секреты, не описанные в реестре, были бы нечитаемы.
func (a *App) secretType(path string) (registry.Type, error) {
	relative := relativeTo(a.Root, path)
	entry := strings.TrimSuffix(relative, filepath.Ext(relative))
	if loaded, err := a.loadRegistry(); err == nil {
		if found, ok := loaded.Entry(entry); ok {
			return found.Type, nil
		}
	}
	switch strings.ToLower(filepath.Ext(relative)) {
	case ".env":
		return registry.TypeEnv, nil
	case ".yaml", ".yml":
		return registry.TypeYAML, nil
	case ".json":
		return registry.TypeJSON, nil
	default:
		return registry.TypeBinary, nil
	}
}

// requireInScope проверяет, что раздел секрета входит в machine_scope.
//
// Вне scope машина секрет не расшифровывает и не показывает: даже при
// активной разблокировке. Настройка объявляет, какие данные этой
// машине нужны.
func (a *App) requireInScope(path string) error {
	config, err := a.machineScope()
	if err != nil {
		return err
	}
	if len(config.MachineScope) == 0 {
		return nil
	}
	section := scopeSection(relativeTo(a.Root, path))
	if section == "" || config.Allows(section) {
		return nil
	}
	return advise(
		fmt.Errorf("раздел %s вне scope этой машины", section),
		ui.KindLock,
		AdviceOutOfScope,
	)
}

// machineScope читает настройку доступа машины.
func (a *App) machineScope() (machine.Config, error) {
	return machine.Load()
}
