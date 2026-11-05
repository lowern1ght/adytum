package adytum

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lowern1ght/adytum/internal/ui"
)

// Значения атрибута filter, которые git возвращает для пути без
// подходящего правила.
const (
	// attrUnspecified — правила для пути нет: файл уйдёт открытым.
	attrUnspecified = "unspecified"
	// attrUnset — правило есть и прямо говорит «не шифровать».
	//
	// Это осознанное исключение владельца (пустой файл, маска):
	// такие файлы коммитить можно, они не секреты.
	attrUnset = "unset"
)

// checkAttrChunk — сколько путей проверяется одним вызовом git.
//
// Длинная командная строка на Windows обрезается: проверку делят на
// части.
const checkAttrChunk = 100

// filterAttribute возвращает значение атрибута filter для пути.
//
// Путь задаётся относительно корня хранилища: правила
// .gitattributes привязаны именно к нему.
func (a *App) filterAttribute(relative string) (string, error) {
	values, err := a.filterAttributes([]string{relative})
	if err != nil {
		return "", err
	}
	value, found := values[relative]
	if !found {
		return "", fmt.Errorf("git check-attr не ответил для %s", relative)
	}
	return value, nil
}

// filterAttributes возвращает значения атрибута filter для набора путей.
//
// Один вызов git на все пути: файлов в хранилище десятки, а запуск
// git на каждый — лишние секунды на каждой операции.
func (a *App) filterAttributes(paths []string) (map[string]string, error) {
	result := map[string]string{}
	for start := 0; start < len(paths); start += checkAttrChunk {
		end := start + checkAttrChunk
		if end > len(paths) {
			end = len(paths)
		}
		values, err := a.filterAttributeChunk(paths[start:end])
		if err != nil {
			return nil, err
		}
		for path, value := range values {
			result[path] = value
		}
	}
	return result, nil
}

// filterAttributeChunk проверяет атрибут filter у части путей.
func (a *App) filterAttributeChunk(paths []string) (map[string]string, error) {
	result := map[string]string{}
	if len(paths) == 0 {
		return result, nil
	}
	args := append([]string{"check-attr", "-z", "filter", "--"}, paths...)
	out, err := a.Runner.Run(Command{Dir: a.Root}, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("не удалось проверить правила шифрования: %w", err)
	}
	// Формат ответа: путь\0атрибут\0значение\0 — по тройке на путь.
	fields := strings.Split(string(out), "\x00")
	for index := 0; index+2 < len(fields); index += 3 {
		result[fields[index]] = fields[index+2]
	}
	return result, nil
}

// requireEncryptedPath проверяет, что файл попадёт в репозиторий
// зашифрованным.
//
// Без правила в .gitattributes шифротекст не создаётся, и файл
// уезжает в remote в открытом виде.
func (a *App) requireEncryptedPath(relative string) error {
	value, err := a.filterAttribute(relative)
	if err != nil {
		return err
	}
	if value == FilterName {
		return nil
	}
	return advise(
		fmt.Errorf("файл %s не покрыт фильтром %s: в репозиторий уйдёт открытым", relative, FilterName),
		ui.KindWarn,
		fmt.Sprintf("добавьте правило в .gitattributes: %s/** filter=%s diff=%s", scopeSection(relative), FilterName, FilterName),
	)
}

// requireEncryptedSecrets проверяет, что ни один файл раздела не уйдёт
// в репозиторий открытым.
//
// Проверка идёт перед коммитом: файлы с исключением (-filter)
// пропускаются — это осознанное решение владельца. Остальное
// покрытие правилом обязательно.
func (a *App) requireEncryptedSecrets() error {
	files := a.sectionFiles()
	if len(files) == 0 {
		return nil
	}
	values, err := a.filterAttributes(files)
	if err != nil {
		return err
	}
	var uncovered []string
	for _, relative := range files {
		value := values[relative]
		if value == FilterName || value == attrUnset {
			continue
		}
		uncovered = append(uncovered, relative)
	}
	if len(uncovered) == 0 {
		return nil
	}
	return advise(
		fmt.Errorf("файлы вне фильтра %s: %s", FilterName, strings.Join(uncovered, ", ")),
		ui.KindWarn,
		fmt.Sprintf("добавьте правило в .gitattributes: %s/** filter=%s diff=%s", scopeSection(uncovered[0]), FilterName, FilterName),
	)
}

// sectionFiles перечисляет файлы секретов по разделам хранилища.
//
// Раздел — каталог верхнего уровня: personal, nova и прочие. Файлы
// корня (registry.yaml, .sops.yaml) секретами не являются: они лежат
// вне разделов и хранятся открытыми.
//
// Пути возвращаются относительно корня хранилища со слэшами: правила
// .gitattributes привязаны именно к таким путям.
func (a *App) sectionFiles() []string {
	var files []string
	err := filepath.WalkDir(a.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path == a.Root {
				return nil
			}
			// Служебные каталоги секретами не являются.
			if name == ".git" || strings.HasPrefix(name, ".") {
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
