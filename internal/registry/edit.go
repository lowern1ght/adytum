package registry

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document — разбираемый документ registry.yaml.
//
// Правки идут через yaml.Node: комментарии и порядок ключей в файле
// сохраняются. В реестре важны комментарии владельца — там описаны
// решения по доступам.
type Document struct {
	doc *yaml.Node
}

// Load читает документ реестра с диска.
func Load(path string) (*Document, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	return ParseDocument(content)
}

// ParseDocument разбирает документ реестра.
func ParseDocument(content []byte) (*Document, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("не удалось разобрать registry.yaml: %w", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, fmt.Errorf("registry.yaml пуст")
	}
	return &Document{doc: &doc}, nil
}

// Bytes сериализует документ обратно в YAML.
func (d *Document) Bytes() ([]byte, error) {
	var builder strings.Builder
	encoder := yaml.NewEncoder(&builder)
	encoder.SetIndent(2)
	if err := encoder.Encode(d.doc); err != nil {
		return nil, fmt.Errorf("не удалось собрать registry.yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("не удалось собрать registry.yaml: %w", err)
	}
	return []byte(builder.String()), nil
}

// Save записывает документ на диск.
func (d *Document) Save(path string) error {
	content, err := d.Bytes()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("не удалось записать %s: %w", path, err)
	}
	return nil
}

// SetRecipient добавляет или заменяет публичный ключ машины.
func (d *Document) SetRecipient(label, public string) error {
	section := d.section("recipients")
	if section == nil {
		return fmt.Errorf("в registry.yaml нет раздела recipients")
	}
	value := mapValue(section, label)
	if value != nil {
		value.Value = public
		value.Tag = "!!str"
		return nil
	}
	setMapValue(section, label, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: public})
	return nil
}

// RemoveRecipient убирает публичный ключ машины.
func (d *Document) RemoveRecipient(label string) {
	section := d.section("recipients")
	if section == nil {
		return
	}
	removeMapValue(section, label)
}

// AddToScope добавляет машину в scope.
func (d *Document) AddToScope(scope, label string) error {
	section := d.section("scopes")
	if section == nil {
		return fmt.Errorf("в registry.yaml нет раздела scopes")
	}
	value := mapValue(section, scope)
	if value == nil {
		value = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setMapValue(section, scope, value)
	}
	for _, item := range value.Content {
		if item.Value == label {
			return nil
		}
	}
	value.Content = append(value.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: label})
	sortSequence(value)
	return nil
}

// RemoveFromScope убирает машину из scope.
//
// Scope без машин не удаляется: записи, ссылающиеся на него, потеряли
// бы определённых получателей и уехали бы под правило по умолчанию —
// под чужих. Оставшийся пустой scope видно в doctor отдельной
// проверкой.
func (d *Document) RemoveFromScope(scope, label string) {
	section := d.section("scopes")
	if section == nil {
		return
	}
	value := mapValue(section, scope)
	if value == nil {
		return
	}
	kept := make([]*yaml.Node, 0, len(value.Content))
	for _, item := range value.Content {
		if item.Value == label {
			continue
		}
		kept = append(kept, item)
	}
	value.Content = kept
}

// SetEntry добавляет или заменяет запись реестра.
//
// Существующая запись обновляется на месте: комментарии к ней
// сохраняются.
func (d *Document) SetEntry(entry Entry) error {
	section := d.section("entries")
	if section == nil {
		return fmt.Errorf("в registry.yaml нет раздела entries")
	}
	node := mapValue(section, entry.Name)
	if node == nil {
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setMapValue(section, entry.Name, node)
	}
	setMapValue(node, "type", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(entry.Type)})
	setMapValue(node, "description", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Description})
	setMapValue(node, "scope", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Scope})
	if entry.Rotates != "" {
		setMapValue(node, "rotates", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Rotates})
	}
	if len(entry.Keys) > 0 {
		keys := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, name := range sortedMapKeys(entry.Keys) {
			setMapValue(keys, name, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Keys[name]})
		}
		setMapValue(node, "keys", keys)
	}
	return nil
}

// RemoveEntry убирает запись реестра.
func (d *Document) RemoveEntry(name string) {
	section := d.section("entries")
	if section == nil {
		return
	}
	removeMapValue(section, name)
}

// HasEntry сообщает, есть ли запись в реестре.
func (d *Document) HasEntry(name string) bool {
	section := d.section("entries")
	if section == nil {
		return false
	}
	return mapValue(section, name) != nil
}

// ScopeNames возвращает имена scopes в алфавитном порядке.
func (d *Document) ScopeNames() []string {
	section := d.section("scopes")
	if section == nil || section.Kind != yaml.MappingNode {
		return nil
	}
	var names []string
	for i := 0; i+1 < len(section.Content); i += 2 {
		names = append(names, section.Content[i].Value)
	}
	sort.Strings(names)
	return names
}

// section возвращает узел раздела документа.
//
// Раздел ищется среди верхнего уровня: recipients, scopes, entries.
func (d *Document) section(name string) *yaml.Node {
	root := d.doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return mapValue(root, name)
}

// mapValue ищет значение ключа в отображении.
func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setMapValue задаёт (или добавляет) значение ключа.
//
// Отображение в строковом стиле переводится в блочный: иначе первая же
// добавленная запись растянула бы раздел в одну длинную строку.
func setMapValue(node *yaml.Node, key string, value *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	if node.Style == yaml.FlowStyle {
		node.Style = 0
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

// removeMapValue убирает ключ из отображения.
func removeMapValue(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != key {
			continue
		}
		node.Content = append(node.Content[:i], node.Content[i+2:]...)
		return
	}
}

// sortSequence сортирует список скаляров.
func sortSequence(node *yaml.Node) {
	sort.SliceStable(node.Content, func(i, j int) bool {
		return node.Content[i].Value < node.Content[j].Value
	})
}

// sortedMapKeys возвращает имена ключей карты в алфавитном порядке.
func sortedMapKeys(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
