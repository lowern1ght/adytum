// Package registry читает и правит registry.yaml — реестр секретов
// хранилища.
//
// Реестр — единственный источник правды о том, какие секреты в
// хранилище есть, какого они типа и кому видны. Файл хранится в
// открытом виде: описания секретов не являются секретами.
//
// Разделение источников правды важно: .sops.yaml генерируется из
// реестра, а не редактируется руками. Два файла, описывающих
// получателей, разъезжаются неизбежно — и об этом должен сообщать
// doctor, а не пользователь.
package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lowern1ght/adytum/internal/secretvalue"
	"gopkg.in/yaml.v3"
)

// FileName — имя файла реестра в корне хранилища.
const FileName = "registry.yaml"

// Version — версия схемы, которую понимает этот код.
const Version = 1

// Type — тип секрета.
//
// Тип определяет формат файла и то, как из него достаётся значение
// по ключу.
type Type string

// Типы секретов.
const (
	// TypeEnv — формат KEY=VALUE.
	TypeEnv Type = "env"
	// TypeYAML — yaml-документ.
	TypeYAML Type = "yaml"
	// TypeJSON — json-документ.
	TypeJSON Type = "json"
	// TypeBinary — бинарный файл, ключи не выделяются.
	TypeBinary Type = "binary"
	// TypeKubeconfig — kubeconfig, ключи не выделяются.
	TypeKubeconfig Type = "kubeconfig"
)

// Types возвращает все поддерживаемые типы в порядке показа.
func Types() []Type {
	return []Type{TypeEnv, TypeYAML, TypeJSON, TypeBinary, TypeKubeconfig}
}

// Valid сообщает, что тип поддерживается.
func (t Type) Valid() bool {
	for _, known := range Types() {
		if t == known {
			return true
		}
	}
	return false
}

// Keyed сообщает, что из файла можно достать значение по ключу.
//
// Для бинарных файлов и kubeconfig выделения ключей нет: там файл
// либо читается целиком, либо не читается вовсе.
func (t Type) Keyed() bool {
	switch t {
	case TypeEnv, TypeYAML, TypeJSON:
		return true
	default:
		return false
	}
}

// Extension возвращает расширение файла по умолчанию для типа.
func (t Type) Extension() string {
	switch t {
	case TypeEnv:
		return ".env"
	case TypeYAML:
		return ".yaml"
	case TypeJSON:
		return ".json"
	default:
		return ".yaml"
	}
}

// Format возвращает формат файла секрета.
//
// Формат нужен операциям над значениями: чтение ключа, запись ключа,
// удаление ключа. Типы без ключей (binary, kubeconfig) формата не
// имеют — значение такого секрета есть весь файл.
func (t Type) Format() secretvalue.Format {
	switch t {
	case TypeEnv:
		return secretvalue.FormatEnv
	case TypeYAML:
		return secretvalue.FormatYAML
	case TypeJSON:
		return secretvalue.FormatJSON
	default:
		return ""
	}
}

// Scope — набор машин, которым виден раздел.
//
// Список машин сопоставляется с публичными ключами age при генерации
// .sops.yaml: scope определяет, кто может расшифровать файл.
type Scope struct {
	// Name — имя scope.
	Name string
	// Machines — метки машин-получателей.
	Machines []string
}

// Entry — запись реестра об одном секрете.
type Entry struct {
	// Name — путь секрета в хранилище: <project>/<service>.
	Name string
	// Type — тип секрета.
	Type Type
	// Description — человеческое описание секрета.
	Description string
	// Scope — scope, определяющий получателей.
	Scope string
	// Rotates — адрес, по которому ротируется значение.
	Rotates string
	// Keys — описания ключей внутри файла.
	//
	// Значения ключей не хранятся: только имена и описания.
	Keys map[string]string
}

// Registry — разобранный реестр секретов.
type Registry struct {
	// Version — версия схемы.
	Version int
	// Recipients — публичные ключи машин: метка → age1-ключ.
	Recipients map[string]string
	// Scopes — scopes по именам.
	Scopes map[string]Scope
	// Entries — записи реестра по именам.
	Entries map[string]Entry
	// Errors — проблемы, найденные при разборе.
	//
	// Битый реестр не должен лишать машину без возможности работать:
	// проблемы собираются, а разбор продолжается.
	Errors []string
}

// New возвращает пустой реестр.
//
// Используется, когда файла реестра нет: записи добавляются в него
// по мере создания секретов.
func New() *Registry {
	return &Registry{
		Version:    Version,
		Recipients: map[string]string{},
		Scopes:     map[string]Scope{},
		Entries:    map[string]Entry{},
	}
}

// Template возвращает заготовку файла реестра.
//
// Заготовка описывает пустое хранилище: разделы recipients, scopes и
// entries есть, но машин в scopes ещё нет. Файл создаётся командой
// `adytum new`, когда реестра ещё нет, — иначе первый секрет негде
// описать.
//
// Scope без машин — не ошибка файла, а невыданный доступ: doctor
// скажет о нём отдельной проверкой.
func Template() []byte {
	return []byte(`# adytum — реестр секретов.
# Файл открытый: описания секретов не являются секретами.
# Правьте его руками или через ` + "`adytum new`" + ` и ` + "`adytum grant`" + `.
version: 1

recipients: {}

scopes:
  personal: []
  default: []

entries: {}
`)
}

// Parse разбирает содержимое registry.yaml.
//
// Битые записи не роняют разбор: они попадают в Errors, а остальной
// реестр остаётся пригодным для показа. Так doctor может показать
// все проблемы сразу, а не одну за раз.
func Parse(data []byte) (*Registry, error) {
	var document struct {
		Version    int                  `yaml:"version"`
		Recipients map[string]string    `yaml:"recipients"`
		Scopes     map[string][]string  `yaml:"scopes"`
		Entries    map[string]yaml.Node `yaml:"entries"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("не удалось разобрать registry.yaml: %w", err)
	}

	registry := &Registry{
		Version:    document.Version,
		Recipients: map[string]string{},
		Scopes:     map[string]Scope{},
		Entries:    map[string]Entry{},
	}
	if registry.Version != Version {
		registry.Errors = append(registry.Errors,
			fmt.Sprintf("версия схемы %d не поддерживается (ожидается %d)", registry.Version, Version))
	}

	for label, public := range document.Recipients {
		registry.Recipients[label] = public
	}

	for name, machines := range document.Scopes {
		scope := Scope{Name: name, Machines: append([]string(nil), machines...)}
		if len(scope.Machines) == 0 {
			registry.Errors = append(registry.Errors,
				fmt.Sprintf("scope %s не содержит машин", name))
		}
		registry.Scopes[name] = scope
	}

	for name, node := range document.Entries {
		entry, problems := parseEntry(name, node)
		registry.Errors = append(registry.Errors, problems...)
		if entry == nil {
			continue
		}
		registry.Entries[name] = *entry
	}
	return registry, nil
}

// parseEntry разбирает одну запись реестра.
//
// Возвращает запись и список проблем. Запись возвращается nil, если
// разобрать её невозможно.
func parseEntry(name string, node yaml.Node) (*Entry, []string) {
	var problems []string

	if node.Kind != yaml.MappingNode {
		return nil, []string{fmt.Sprintf("запись %s: ожидался объект с полями", name)}
	}

	var raw struct {
		Type        string            `yaml:"type"`
		Description string            `yaml:"description"`
		Scope       string            `yaml:"scope"`
		Rotates     string            `yaml:"rotates"`
		Keys        map[string]string `yaml:"keys"`
	}
	if err := node.Decode(&raw); err != nil {
		return nil, []string{fmt.Sprintf("запись %s: не удалось разобрать поля: %v", name, err)}
	}

	entry := Entry{
		Name:        name,
		Type:        Type(raw.Type),
		Description: raw.Description,
		Scope:       raw.Scope,
		Rotates:     raw.Rotates,
		Keys:        raw.Keys,
	}

	if entry.Type == "" {
		problems = append(problems, fmt.Sprintf("запись %s: не указан type", name))
	} else if !entry.Type.Valid() {
		problems = append(problems, fmt.Sprintf("запись %s: неизвестный type %q (допустимо: %s)",
			name, raw.Type, typeList()))
	}
	if entry.Scope == "" {
		problems = append(problems, fmt.Sprintf("запись %s: не указан scope", name))
	}
	if entry.Description == "" {
		problems = append(problems, fmt.Sprintf("запись %s: не указан description", name))
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return &entry, nil
}

// typeList возвращает список типов для сообщений об ошибках.
func typeList() string {
	names := make([]string, 0, len(Types()))
	for _, entry := range Types() {
		names = append(names, string(entry))
	}
	return strings.Join(names, "|")
}

// ScopeNames возвращает имена scopes в алфавитном порядке.
func (r *Registry) ScopeNames() []string {
	names := make([]string, 0, len(r.Scopes))
	for name := range r.Scopes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// EntryNames возвращает имена записей в алфавитном порядке.
func (r *Registry) EntryNames() []string {
	names := make([]string, 0, len(r.Entries))
	for name := range r.Entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Scope возвращает scope по имени.
func (r *Registry) Scope(name string) (Scope, bool) {
	scope, found := r.Scopes[name]
	return scope, found
}

// Entry возвращает запись реестра по имени.
func (r *Registry) Entry(name string) (Entry, bool) {
	entry, found := r.Entries[name]
	return entry, found
}

// HasEntry сообщает, есть ли запись в реестре.
func (r *Registry) HasEntry(name string) bool {
	_, found := r.Entries[name]
	return found
}

// UnknownScopes возвращает scope'ы записей, которых нет в scopes.
//
// Такой scope не определяет получателей: файл с ним зашифровать
// нечем.
func (r *Registry) UnknownScopes() []string {
	seen := map[string]bool{}
	var unknown []string
	for _, entry := range r.Entries {
		if entry.Scope == "" {
			continue
		}
		if _, found := r.Scopes[entry.Scope]; found {
			continue
		}
		if seen[entry.Scope] {
			continue
		}
		seen[entry.Scope] = true
		unknown = append(unknown, entry.Scope)
	}
	sort.Strings(unknown)
	return unknown
}

// UnknownMachines возвращает метки машин, для которых нет публичного
// ключа.
//
// Такая машина не сможет расшифровать раздел: scope с ней не
// генерируется, и doctor обязан об этом сказать.
//
// Ключи берутся из раздела recipients реестра и локального реестра
// ~/.adytum/recipients: машина без своего ключа в локальном реестре
// не считается потерянной.
func (r *Registry) UnknownMachines(local map[string]string) []string {
	resolved := r.ResolveRecipients(local)
	seen := map[string]bool{}
	var unknown []string
	for _, scope := range r.Scopes {
		for _, machine := range scope.Machines {
			if resolved[machine] != "" {
				continue
			}
			if seen[machine] {
				continue
			}
			seen[machine] = true
			unknown = append(unknown, machine)
		}
	}
	sort.Strings(unknown)
	return unknown
}
