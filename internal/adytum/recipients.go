package adytum

import (
	"os"
	"path/filepath"
	"regexp"

	"github.com/lowern1ght/adytum/internal/sopsconf"
	"github.com/lowern1ght/adytum/internal/store"
)

// recipientState — состояние доступа машины к разделу.
type recipientState int

const (
	// recipientUnknown — состояние определить не удалось.
	recipientUnknown recipientState = iota
	// recipientAbsent — машины нет в получателях раздела: штатная
	// ситуация, раздел ей не положен.
	recipientAbsent
	// recipientPresent — машина в получателях раздела: расшифровка
	// обязана была удаться.
	recipientPresent
)

// recipientStateOf определяет, входит ли машина в получателей файла.
//
// Различие важно: «машины нет в правиле» — штатное состояние, а
// «машина в правиле, но расшифровка не удалась» — признак порчи
// файла или перешифровки без этой машины. Второе — авария, и молчать
// о нём нельзя.
func (a *App) recipientStateOf(path string) recipientState {
	public, err := a.publicKey()
	if err != nil {
		return recipientUnknown
	}
	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err != nil {
		return recipientUnknown
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		return recipientUnknown
	}
	relative := relativeTo(a.Root, path)
	for _, rule := range config.Rules() {
		matched, err := regexp.MatchString(rule.PathRegex, relative)
		if err != nil || !matched {
			continue
		}
		// Первое подошедшее правило и есть действующее: так же
		// выбирает правило сам sops.
		for _, recipient := range rule.Age {
			if recipient == public {
				return recipientPresent
			}
		}
		return recipientAbsent
	}
	return recipientAbsent
}

// publicKey возвращает публичный ключ машины.
//
// Ключ лежит рядом с приватным: он и есть идентичность машины в
// правилах шифрования.
func (a *App) publicKey() (string, error) {
	label, err := store.IdentityLabel()
	if err != nil {
		return "", err
	}
	keyFile, err := store.KeyFile(label)
	if err != nil {
		return "", err
	}
	return store.PublicKeyFromIdentity(keyFile)
}

// SecretFileExists сообщает, есть ли файл секрета по адресу.
//
// Адрес может называть файл целиком (имя сервиса с точками) или файл с
// расширением: без такой проверки имя с точками читалось бы как ключ.
func (a *App) SecretFileExists(project, service string) bool {
	_, _, err := ResolveSecret(a.Root, project, service)
	return err == nil
}

// relativePath возвращает путь файла относительно корня хранилища со
// слэшами.
//
// Разделитель приводится к слэшу: правила шифрования написаны под
// unix-пути, и на Windows путь с обратными слэшами под правило не
// попал бы.
func relativePath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}
