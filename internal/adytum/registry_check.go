package adytum

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/sopsconf"
)

// registryChecks сверяет реестр секретов с диском и .sops.yaml.
//
// Сверка ловит расхождения, которые иначе обнаруживаются только при
// следующей перешифровке: запись без файла, файл без записи, scope
// без определения и разошедшиеся получатели.
func (a *App) registryChecks() []Check {
	loaded, err := a.loadRegistry()
	if err != nil {
		check := Check{Name: "реестр"}
		if errors.Is(err, ErrRegistryMissing) {
			check.Detail = "реестра нет: " + a.RegistryPath()
			check.Fix = "создайте реестр: adytum new <project>/<service>"
			return []Check{check}
		}
		check.Detail = err.Error()
		check.Fix = "исправьте registry.yaml"
		return []Check{check}
	}

	checks := []Check{
		a.checkRegistryErrors(loaded),
		a.checkRegistryEntries(loaded),
		a.checkRegistryFiles(loaded),
		a.checkRegistryScopes(loaded),
		a.checkSopsSync(loaded),
	}
	return checks
}

// checkRegistryErrors показывает проблемы, найденные при разборе.
//
// Проблемы не роняют разбор: остальной реестр остаётся пригодным для
// показа. Но молчать о них нельзя — иначе битый секрет выглядит как
// рабочий.
func (a *App) checkRegistryErrors(loaded *registry.Registry) Check {
	check := Check{Name: "реестр: разбор"}
	if len(loaded.Errors) == 0 {
		check.OK = true
		check.Detail = "проблем нет"
		return check
	}
	check.Detail = strings.Join(loaded.Errors, "; ")
	check.Fix = "исправьте registry.yaml"
	return check
}

// checkRegistryEntries проверяет, что у каждой записи реестра есть
// файл на диске.
//
// Запись без файла — обман: секрет в ней описан, но его нет, и
// владелец посчитает, что он зашифрован.
func (a *App) checkRegistryEntries(loaded *registry.Registry) Check {
	check := Check{Name: "реестр: записи"}

	var missing []string
	for _, name := range loaded.EntryNames() {
		entry := loaded.Entries[name]
		if a.findSecretFile(entry.Name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		check.Detail = "нет файла: " + strings.Join(missing, ", ")
		check.Fix = "создайте файлы: adytum new <project>/<service>"
		return check
	}
	check.OK = true
	check.Detail = fmt.Sprintf("записей: %d, все на месте", len(loaded.Entries))
	return check
}

// checkRegistryFiles проверяет, что каждый файл хранилища описан в
// реестре.
//
// Незарегистрированный файл — главный риск реестра: о нём не знает
// ни владелец, ни получатель, а он лежит в хранилище.
func (a *App) checkRegistryFiles(loaded *registry.Registry) Check {
	check := Check{Name: "реестр: файлы"}

	files := a.storeFiles()
	var unregistered []string
	for _, file := range files {
		name := strings.TrimSuffix(file, filepath.Ext(file))
		if _, found := loaded.Entry(name); found {
			continue
		}
		unregistered = append(unregistered, file)
	}
	if len(unregistered) > 0 {
		check.Detail = "незарегистрированные: " + strings.Join(unregistered, ", ")
		check.Fix = "опишите их в registry.yaml"
		return check
	}
	check.OK = true
	check.Detail = fmt.Sprintf("файлов: %d, все описаны", len(files))
	return check
}

// checkRegistryScopes проверяет, что scope каждой записи определён.
//
// Scope без определения не определяет получателей: файл с таким
// scope зашифровать нечем.
func (a *App) checkRegistryScopes(loaded *registry.Registry) Check {
	check := Check{Name: "реестр: scopes"}

	unknown := loaded.UnknownScopes()
	if len(unknown) > 0 {
		check.Detail = "не определены: " + strings.Join(unknown, ", ")
		check.Fix = "добавьте их в раздел scopes registry.yaml"
		return check
	}
	var empty []string
	for _, name := range loaded.ScopeNames() {
		if len(loaded.Scopes[name].Machines) == 0 {
			empty = append(empty, name)
		}
	}
	if len(empty) > 0 {
		check.Detail = "без машин: " + strings.Join(empty, ", ")
		check.Fix = "выдайте им доступ: adytum grant <label> --scope <scope>"
		return check
	}
	missing := loaded.UnknownMachines(a.localRecipients())
	if len(missing) > 0 {
		check.Detail = "нет публичного ключа для машин: " + strings.Join(missing, ", ")
		check.Fix = "выдайте им доступ: adytum grant <label>"
		return check
	}
	check.OK = true
	check.Detail = fmt.Sprintf("scopes: %d", len(loaded.Scopes))
	return check
}

// checkSopsSync сверяет получателей .sops.yaml с scopes реестра.
//
// Расхождение означает, что правила шифрования разошлись с
// реестром: кто-то получил доступ в обход реестра или потерял его.
func (a *App) checkSopsSync(loaded *registry.Registry) Check {
	check := Check{Name: ".sops.yaml ↔ реестр"}

	content, err := os.ReadFile(SopsConfigPath(a.Root))
	if err != nil {
		check.Detail = ".sops.yaml не найден"
		check.Fix = "adytum reencrypt"
		return check
	}
	config, err := sopsconf.Parse(content)
	if err != nil {
		check.Detail = err.Error()
		check.Fix = "adytum reencrypt"
		return check
	}

	recipients := loaded.ResolveRecipients(a.localRecipients())
	expected, err := registry.SopsRules(loaded.Scopes, recipients)
	if err != nil {
		check.Detail = err.Error()
		check.Fix = "выдайте доступ машинам без ключа: adytum grant <label>"
		return check
	}

	actual := map[string][]string{}
	for _, rule := range config.Rules() {
		actual[rule.PathRegex] = rule.Age
	}

	var mismatched []string
	for pathRegex, age := range expected {
		current, found := actual[pathRegex]
		if !found {
			mismatched = append(mismatched, pathRegex)
			continue
		}
		if !sameRecipients(current, age) {
			mismatched = append(mismatched, pathRegex)
		}
	}
	for pathRegex := range actual {
		if _, found := expected[pathRegex]; !found {
			mismatched = append(mismatched, pathRegex)
		}
	}
	if len(mismatched) > 0 {
		sort.Strings(mismatched)
		check.Detail = "рассинхрон по правилам: " + strings.Join(mismatched, ", ")
		check.Fix = "запустите adytum reencrypt"
		return check
	}
	check.OK = true
	check.Detail = "получатели совпадают"
	return check
}

// sameRecipients сравнивает списки публичных ключей без учёта порядка.
func sameRecipients(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := map[string]int{}
	for _, key := range left {
		seen[key]++
	}
	for _, key := range right {
		seen[key]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}

// storeFiles перечисляет файлы секретов в хранилище.
//
// Возвращаются пути относительно корня: вида personal/github.env.
// Файлы корня (registry.yaml, .sops.yaml) секретами не являются: они
// лежат вне разделов и в реестре не описываются.
func (a *App) storeFiles() []string {
	var files []string
	err := filepath.Walk(a.Root, func(path string, entry os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(a.Root, path)
		if err != nil {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if !strings.Contains(relative, "/") {
			return nil
		}
		if !IsSecretPath(relative) {
			return nil
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return files
	}
	sort.Strings(files)
	return files
}

// findSecretFile ищет файл секрета по записи реестра.
//
// Запись реестра не содержит расширения: файл ищется по всем
// допустимым расширениям.
func (a *App) findSecretFile(entry string) string {
	for _, ext := range []string{".env", ".yaml", ".yml", ".json"} {
		path := filepath.Join(a.Root, filepath.FromSlash(entry)+ext)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return filepath.ToSlash(entry + ext)
		}
	}
	return ""
}
