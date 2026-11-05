package registry

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultScope — scope, покрывающий все файлы без частного scope.
const DefaultScope = "default"

// SopsRule — правило creation_rules для одного scope.
type SopsRule struct {
	// PathRegex — регулярка пути файла.
	PathRegex string
	// Age — публичные ключи получателей.
	Age []string
}

// ResolveRecipients возвращает карту «метка → публичный ключ».
//
// Источники: раздел recipients реестра, затем локальный реестр
// ~/.adytum/recipients. Раздел реестра важнее: он едет вместе с
// хранилищем, и без него правила шифрования не собрать на новой
// машине.
func (r *Registry) ResolveRecipients(local map[string]string) map[string]string {
	resolved := map[string]string{}
	for label, public := range local {
		resolved[label] = public
	}
	for label, public := range r.Recipients {
		if public == "" {
			continue
		}
		resolved[label] = public
	}
	return resolved
}

// GenerateSopsConfig собирает .sops.yaml из scopes реестра.
//
// Реестр — единственный источник правды: правила шифрования
// выводятся из него, а не редактируются руками. Иначе получатели
// разъезжаются с описанием в реестре, и это обнаруживается только
// при следующей перешифровке.
//
// Путь правила выводится из имени scope: scope «personal» покрывает
// каталог personal/, scope «default» — всё остальное. Порядок
// правил сохраняется, поэтому более специфичные идут первыми.
//
// Scope без машин правила не получает: получать ему нечем. Если на
// такой scope ссылаются записи, перешифровка их откажет — иначе они
// уехали бы под правило по умолчанию и получили чужих получателей.
func GenerateSopsConfig(scopes map[string]Scope, recipients map[string]string) ([]byte, error) {
	names := make([]string, 0, len(scopes))
	for name := range scopes {
		names = append(names, name)
	}
	sort.Strings(names)

	// Правило по умолчанию идёт последним: sops берёт первое
	// подошедшее правило, и «default» перехватывал бы частные
	// разделы.
	ordered := make([]string, 0, len(names))
	for _, name := range names {
		if name == DefaultScope {
			continue
		}
		ordered = append(ordered, name)
	}
	if _, found := scopes[DefaultScope]; found {
		ordered = append(ordered, DefaultScope)
	}

	rules := make([]SopsRule, 0, len(ordered))
	for _, name := range ordered {
		scope := scopes[name]
		if len(scope.Machines) == 0 {
			continue
		}
		age := make([]string, 0, len(scope.Machines))
		for _, machine := range scope.Machines {
			public, found := recipients[machine]
			if !found || public == "" {
				return nil, fmt.Errorf("scope %s: для машины %s нет публичного ключа", name, machine)
			}
			age = append(age, public)
		}
		rules = append(rules, SopsRule{
			PathRegex: pathRegex(name),
			Age:       age,
		})
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("в реестре нет scopes с получателями: нечего генерировать")
	}
	return marshalSopsConfig(rules)
}

// pathRegex возвращает регулярку пути для scope.
//
// Для scope «personal» это ^personal[\\/]: класс символов покрывает
// оба разделителя пути, иначе правило не сработало бы на Windows.
func pathRegex(scope string) string {
	if scope == DefaultScope {
		return "^.*$"
	}
	return "^" + scope + "[\\\\/]"
}

// marshalSopsConfig собирает документ .sops.yaml.
//
// Правила пишутся вручную, а не через yaml-энкодер: регулярки путей
// содержат обратные слэши, и yaml-энкодер их экранирует нечитаемо.
func marshalSopsConfig(rules []SopsRule) ([]byte, error) {
	var builder strings.Builder
	builder.WriteString("# adytum — правила шифрования sops+age.\n")
	builder.WriteString("# Файл сгенерирован из registry.yaml: не правьте руками,\n")
	builder.WriteString("# изменения вносятся в реестр, затем выполняется `adytum reencrypt`.\n")
	builder.WriteString("creation_rules:\n")
	for _, item := range rules {
		builder.WriteString("  - path_regex: '" + item.PathRegex + "'\n")
		builder.WriteString("    age:\n")
		for _, key := range item.Age {
			builder.WriteString("      - " + key + "\n")
		}
	}
	return []byte(builder.String()), nil
}

// SopsRules собирает ожидаемые правила creation_rules.
//
// Карта нужна для сверки реестра с .sops.yaml: расхождение означает,
// что правила шифрования разошлись с реестром.
//
// Scope без машин правила не получает — так же, как при генерации.
func SopsRules(scopes map[string]Scope, recipients map[string]string) (map[string][]string, error) {
	result := map[string][]string{}
	for name, scope := range scopes {
		if len(scope.Machines) == 0 {
			continue
		}
		age := make([]string, 0, len(scope.Machines))
		for _, machine := range scope.Machines {
			public, found := recipients[machine]
			if !found || public == "" {
				return nil, fmt.Errorf("scope %s: для машины %s нет публичного ключа", name, machine)
			}
			age = append(age, public)
		}
		result[pathRegex(name)] = age
	}
	return result, nil
}
