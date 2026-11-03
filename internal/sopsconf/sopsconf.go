// Package sopsconf читает и правит .sops.yaml — конфигурацию sops с
// creation_rules и списком age-получателей.
//
// Правки идут через yaml.Node, поэтому комментарии и порядок ключей в
// файле сохраняются: в .sops.yaml важны комментарии владельца репозитория.
package sopsconf

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Placeholder — префикс ключа-заглушки в шаблоне .sops.yaml.
const Placeholder = "age1REPLACE_ME"

// Rule — правило создания: путь и список age-получателей.
type Rule struct {
	PathRegex string
	Age       []string
}

// File — разобранный .sops.yaml.
type File struct {
	doc *yaml.Node
}

// Parse разбирает содержимое .sops.yaml.
func Parse(data []byte) (*File, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("не удалось разобрать .sops.yaml: %w", err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, fmt.Errorf(".sops.yaml пуст")
	}
	return &File{doc: &doc}, nil
}

// Rules возвращает правила создания в порядке файла.
func (f *File) Rules() []Rule {
	var rules []Rule
	for _, node := range f.rulesNodes() {
		rule := Rule{PathRegex: scalar(mapValue(node, "path_regex"))}
		for _, age := range ageValues(mapValue(node, "age")) {
			rule.Age = append(rule.Age, age)
		}
		rules = append(rules, rule)
	}
	return rules
}

// Recipients возвращает уникальные получателей всех правил.
func (f *File) Recipients() []string {
	seen := map[string]bool{}
	var out []string
	for _, rule := range f.Rules() {
		for _, age := range rule.Age {
			if age == "" || seen[age] {
				continue
			}
			seen[age] = true
			out = append(out, age)
		}
	}
	return out
}

// ReplacePlaceholders подставляет реальные публичные ключи вместо
// заглушек вида age1REPLACE_ME_PC.
//
// Возвращает true, если что-то изменилось. Заглушки без известного ключа
// остаются на месте: слот роли (ПК, apiarion, recovery) нельзя терять —
// его заполнит `adytum grant`, когда ключ появится.
func (f *File) ReplacePlaceholders(recipients map[string]string) bool {
	changed := false
	for _, node := range f.rulesNodes() {
		age := mapValue(node, "age")
		if age == nil || age.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range age.Content {
			label, isPlaceholder := placeholderLabel(item.Value)
			if !isPlaceholder {
				continue
			}
			public, found := lookupRecipient(recipients, label)
			if !found || public == "" {
				continue
			}
			item.Value = public
			changed = true
		}
	}
	return changed
}

// UnresolvedPlaceholders возвращает метки заглушек без известного ключа.
//
// Пока список не пуст, sops не сможет прочитать конфигурацию: битые
// получатели в правилах ломают шифрование целиком.
func (f *File) UnresolvedPlaceholders(recipients map[string]string) []string {
	var labels []string
	seen := map[string]bool{}
	for _, node := range f.rulesNodes() {
		age := mapValue(node, "age")
		if age == nil || age.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range age.Content {
			label, isPlaceholder := placeholderLabel(item.Value)
			if !isPlaceholder || seen[label] {
				continue
			}
			if public, found := lookupRecipient(recipients, label); found && public != "" {
				continue
			}
			seen[label] = true
			labels = append(labels, label)
		}
	}
	return labels
}

// ReplaceOrAddRecipient ставит получателя в правило.
//
// Если в правиле есть заглушка с такой же меткой, она заменяется;
// иначе получатель добавляется в конец списка.
func (f *File) ReplaceOrAddRecipient(pathRegex, recipient, label string) (bool, error) {
	if recipient == "" {
		return false, fmt.Errorf("пустой получатель")
	}
	for _, node := range f.rulesNodes() {
		if scalar(mapValue(node, "path_regex")) != pathRegex {
			continue
		}
		age := mapValue(node, "age")
		if age == nil {
			age = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setMapValue(node, "age", age)
		}
		if age.Kind != yaml.SequenceNode {
			return false, fmt.Errorf("правило %s: age должен быть списком", pathRegex)
		}
		for _, item := range age.Content {
			if item.Value == recipient {
				return false, nil
			}
		}
		if label != "" {
			for _, item := range age.Content {
				placeholder, isPlaceholder := placeholderLabel(item.Value)
				if isPlaceholder && strings.EqualFold(placeholder, label) {
					item.Value = recipient
					return true, nil
				}
			}
		}
		age.Content = append(age.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: recipient})
		return true, nil
	}
	return false, fmt.Errorf("правило с path_regex %q не найдено", pathRegex)
}

// IsPersonalRule сообщает, относится ли правило к закрытому разделу.
//
// Раздел personal/** доступен только владельцу и recovery-машине: новая
// машина туда не попадает, пока владелец не решит иначе.
func IsPersonalRule(pathRegex string) bool {
	return strings.Contains(strings.ToLower(pathRegex), "personal")
}

// AddRecipient добавляет получателя в правило с указанным path_regex.
// Возвращает false, если правило не найдено или получатель уже есть.
func (f *File) AddRecipient(pathRegex, recipient string) (bool, error) {
	if recipient == "" {
		return false, fmt.Errorf("пустой получатель")
	}
	for _, node := range f.rulesNodes() {
		if scalar(mapValue(node, "path_regex")) != pathRegex {
			continue
		}
		age := mapValue(node, "age")
		if age == nil {
			age = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setMapValue(node, "age", age)
		}
		if age.Kind != yaml.SequenceNode {
			return false, fmt.Errorf("правило %s: age должен быть списком", pathRegex)
		}
		for _, item := range age.Content {
			if item.Value == recipient {
				return false, nil
			}
		}
		age.Content = append(age.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: recipient})
		return true, nil
	}
	return false, fmt.Errorf("правило с path_regex %q не найдено", pathRegex)
}

// RemoveRecipient убирает получателя из всех правил. Возвращает число
// удалённых вхождений.
func (f *File) RemoveRecipient(recipient string) int {
	removed := 0
	for _, node := range f.rulesNodes() {
		age := mapValue(node, "age")
		if age == nil || age.Kind != yaml.SequenceNode {
			continue
		}
		kept := age.Content[:0]
		for _, item := range age.Content {
			if item.Value == recipient {
				removed++
				continue
			}
			kept = append(kept, item)
		}
		age.Content = kept
	}
	return removed
}

// Bytes сериализует документ обратно в YAML.
func (f *File) Bytes() ([]byte, error) {
	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(f.doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// rulesNodes возвращает узлы элементов creation_rules.
func (f *File) rulesNodes() []*yaml.Node {
	root := f.doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	rules := mapValue(root, "creation_rules")
	if rules == nil || rules.Kind != yaml.SequenceNode {
		return nil
	}
	return rules.Content
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

// setMapValue задаёт (или добавляет) значение ключа в отображении.
func setMapValue(node *yaml.Node, key string, value *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
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

// scalar возвращает значение скаляра.
func scalar(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

// ageValues возвращает список получателей: либо как есть (список), либо
// разделённый по запятой (скаляр).
func ageValues(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.SequenceNode:
		var out []string
		for _, item := range node.Content {
			out = append(out, item.Value)
		}
		return out
	case yaml.ScalarNode:
		var out []string
		for _, part := range strings.Split(node.Value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	return nil
}

var placeholderPattern = regexp.MustCompile(`^` + Placeholder + `(?:_([A-Za-z0-9_-]+))?$`)

// placeholderLabel извлекает метку из заглушки: age1REPLACE_ME_PC -> PC.
func placeholderLabel(value string) (string, bool) {
	match := placeholderPattern.FindStringSubmatch(value)
	if match == nil {
		return "", false
	}
	return match[1], true
}

// lookupRecipient ищет публичный ключ по метке, игнорируя регистр.
//
// Метки в реестре пишутся как удобно ("pc", "PC"), а в .sops.yaml
// заглушки — в верхнем регистре ("age1REPLACE_ME_PC").
func lookupRecipient(recipients map[string]string, label string) (string, bool) {
	if label == "" {
		return "", false
	}
	if public, found := recipients[label]; found {
		return public, true
	}
	for name, public := range recipients {
		if strings.EqualFold(name, label) {
			return public, true
		}
	}
	return "", false
}
