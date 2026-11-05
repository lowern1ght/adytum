package adytum

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/store"
	"github.com/lowern1ght/adytum/internal/ui"
)

// Reencrypt перешифровывает все секреты по правилам реестра и
// фиксирует результат коммитом.
//
// Порядок работы:
//
//  1. проверяется, что реестр согласован: у каждого scope есть
//     получатели, а каждый scope существует;
//  2. из registry.yaml генерируется .sops.yaml — реестр единственный
//     источник правды о получателях;
//  3. файлы перешифровываются по разделам: каждый раздел со своими
//     получателями;
//  4. результат коммитится: незакоммиченный .sops.yaml и
//     перешифрованные блобы оставили бы индекс в промежуточном
//     состоянии, и следующий sync спотыкался бы о него.
//
// Требует активной разблокировки или явного подтверждения: команда
// меняет доступ к данным, и делать её молча нельзя.
func (a *App) Reencrypt(confirmed bool) error {
	if err := a.requireReencryptPermission(confirmed); err != nil {
		return err
	}
	sections, err := a.reencryptAll()
	if err != nil {
		return err
	}
	if err := a.commitAndPush(reencryptMessage(sections)); err != nil {
		return err
	}
	a.hint("проверьте результат: adytum doctor")
	return nil
}

// reencryptAll перешифровывает файлы по правилам реестра.
//
// Правки остаются в рабочем дереве и индексе: коммит делает
// вызывающий — grant и revoke коммитят вместе со своими изменениями.
//
// Возвращаются разделы, которые были перешифрованы.
func (a *App) reencryptAll() ([]string, error) {
	loaded, err := a.loadRegistry()
	if err != nil {
		return nil, a.registryError(err)
	}
	if err := requireRegistryConsistent(loaded, ""); err != nil {
		return nil, err
	}
	recipients := loaded.ResolveRecipients(a.localRecipients())

	content, err := registry.GenerateSopsConfig(loaded.Scopes, recipients)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(SopsConfigPath(a.Root), content, 0o644); err != nil {
		return nil, fmt.Errorf("не удалось записать .sops.yaml: %w", err)
	}
	a.println(".sops.yaml сгенерирован из registry.yaml")

	sections, err := a.reencryptSections(loaded)
	if err != nil {
		return nil, err
	}
	return sections, nil
}

// reencryptPrefix — начало сообщения коммита перешифровки.
//
// Формат сообщения задан правилами репозитория: тег, фича, двоеточие.
const reencryptPrefix = "[.stbl](feat/adytum): reencrypt "

// reencryptMessage собирает сообщение коммита для перешифровки.
//
// Имена разделов в сообщении нужны владельцу: по ним видно, какие
// файлы сменили набор получателей. Длинный список не помещается в
// тему коммита — тогда остаётся его длина.
func reencryptMessage(sections []string) string {
	if len(sections) == 0 {
		return reencryptPrefix + "secrets"
	}
	joined := strings.Join(sections, ", ")
	if len(reencryptPrefix)+len(joined) <= 72 {
		return reencryptPrefix + joined
	}
	return fmt.Sprintf("%s%d sections", reencryptPrefix, len(sections))
}

// requireReencryptPermission проверяет, что операция разрешена.
//
// Перешифровка меняет, кто может читать данные. Случайный запуск
// команды с полным доступом к машине тихо отозвал бы доступ у
// получателей.
func (a *App) requireReencryptPermission(confirmed bool) error {
	if confirmed {
		return nil
	}
	state, err := store.Unlock(a.Now())
	if err != nil {
		return err
	}
	if state.Unlocked {
		return nil
	}
	return advise(
		fmt.Errorf("перешифровка меняет доступ к данным"),
		ui.KindLock,
		"разблокируйте операцию: adytum unlock (или подтвердите: adytum reencrypt --yes)",
	)
}

// reencryptSections перешифровывает файлы по разделам.
//
// Раздел перешифровывается отдельно: так видно, какой раздел не
// удалось перешифровать, и правила для personal/** не применяются к
// файлам других разделов.
func (a *App) reencryptSections(loaded *registry.Registry) ([]string, error) {
	sections := a.sectionsToReencrypt(loaded)
	if len(sections) == 0 {
		a.println("перешифровывать нечего")
		return nil, nil
	}
	for _, section := range sections {
		if err := a.reencryptSection(section); err != nil {
			return nil, fmt.Errorf("раздел %s: %w", section, err)
		}
		a.printf("раздел %s перешифрован\n", section)
	}
	return sections, nil
}

// sectionsToReencrypt перечисляет разделы с файлами.
//
// Список берётся из реестра: раздел без записей перешифровывать
// нечего.
func (a *App) sectionsToReencrypt(loaded *registry.Registry) []string {
	seen := map[string]bool{}
	var sections []string
	for _, name := range loaded.EntryNames() {
		section := secretSection(name)
		if section == "" || seen[section] {
			continue
		}
		seen[section] = true
		sections = append(sections, section)
	}
	return sections
}

// reencryptSection перешифровывает файлы одного раздела.
//
// Файлы раздела прогоняются через clean-фильтр git заново: он берёт
// правила из обновлённого .sops.yaml.
func (a *App) reencryptSection(section string) error {
	// Путь для git — со слэшами: обратный слэш в pathspec git на
	// Windows не понимает.
	pattern := filepath.ToSlash(filepath.Join(section, "*"))
	if _, err := a.Runner.Run(Command{Dir: a.Root}, "git", "add", "--renormalize", "--", pattern); err != nil {
		return fmt.Errorf("не удалось перешифровать файлы: %w", err)
	}
	return nil
}

// secretSection возвращает раздел секрета по записи реестра.
//
// Запись реестра — путь вида personal/github, его раздел — первый
// компонент пути.
func secretSection(entry string) string {
	parts := strings.SplitN(filepath.ToSlash(entry), "/", 2)
	if len(parts) < 2 || parts[0] == "" {
		return ""
	}
	return parts[0]
}
