package adytum

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lowern1ght/adytum/internal/filters"
)

// FilterName — имя git-фильтра шифрования.
//
// Фильтр объявлен в .gitattributes: без него файл попадает в
// репозиторий открытым.
const FilterName = "adytum"

// Gitattributes — содержимое .gitattributes, которое ставит adytum init.
//
// Порядок важен: последняя подходящая строка побеждает, поэтому
// исключения для служебных файлов идут после общих правил.
const Gitattributes = `# adytum: секреты шифруются sops+age при записи в git.
# В рабочем дереве файл остаётся открытым, в репозитории — шифротекст.
*.env filter=adytum diff=adytum
*.yaml filter=adytum diff=adytum
*.yml filter=adytum diff=adytum
*.json filter=adytum diff=adytum

# Конфигурация sops должна оставаться открытой, иначе sops не прочитает
# правила шифрования.
.sops.yaml -filter -diff

# Служебные файлы репозитория не шифруются.
.gitattributes -filter -diff
go.mod -filter -diff
go.sum -filter -diff
`

// SopsTemplate — шаблон .sops.yaml с ключами-заглушками.
//
// age1REPLACE_ME_* подставляются при `adytum init` из ~/.adytum/recipients.
const SopsTemplate = `# adytum — правила шифрования sops+age.
#
# Ключи-заглушки age1REPLACE_ME_* заполняются при ` + "`adytum init`" + ` из
# ~/.adytum/recipients (метка=публичный ключ).
#
# personal/** доступен только машине владельца и recovery-машине,
# остальные разделы — также машине apiarion.
creation_rules:
  - path_regex: '^personal[\\/]'
    age:
      - age1REPLACE_ME_PC
      - age1REPLACE_ME_RECOVERY
  - path_regex: '^.*$'
    age:
      - age1REPLACE_ME_PC
      - age1REPLACE_ME_APIARION
      - age1REPLACE_ME_RECOVERY
`

// GitattributesPath — путь к .gitattributes в корне репозитория.
func GitattributesPath(root string) string {
	return filepath.Join(root, ".gitattributes")
}

// SopsConfigPath — путь к .sops.yaml в корне репозитория.
func SopsConfigPath(root string) string {
	return filepath.Join(root, ".sops.yaml")
}

// ensureGitattributes дописывает блок adytum в .gitattributes, если его нет.
//
// Существующие правила не трогаются: файл дополняется целиком.
func ensureGitattributes(root string) (bool, error) {
	path := GitattributesPath(root)
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(current), "filter=adytum") {
		return false, nil
	}
	var builder strings.Builder
	if len(current) > 0 {
		builder.Write(current)
		if !strings.HasSuffix(string(current), "\n") {
			builder.WriteString("\n")
		}
	}
	builder.WriteString(Gitattributes)
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// ensureSopsConfig создаёт .sops.yaml из шаблона, если его нет.
func ensureSopsConfig(root string) (bool, error) {
	path := SopsConfigPath(root)
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(path, []byte(SopsTemplate), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// gitConfigValues — значения конфигурации git, которые adytum ставит.
//
// Значения фильтров задаёт пакет filters: там же живут скрипты-обёртки,
// которые эти значения вызывают.
func gitConfigValues(scripts filters.Scripts) map[string]string {
	values := filters.ConfigValues(scripts)
	values["diff.adytum.textconv"] = "adytum diff-convert"
	return values
}
