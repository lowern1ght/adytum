package sopsconf

import (
	"strings"
	"testing"
)

// template — тестовый .sops.yaml в форме, которую генерирует adytum.
const template = `# комментарий сохраняется
creation_rules:
  - path_regex: '^personal[\\/]'
    age:
      - age1REPLACE_ME_PC
      - age1REPLACE_ME_RECOVERY
  - path_regex: '^.*$'
    age:
      - age1REPLACE_ME_PC
      - age1REPLACE_ME_APIARION
`

func TestParseReadsCreationRules(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rules := file.Rules()
	if len(rules) != 2 {
		t.Fatalf("ожидалось 2 правила, получено %d", len(rules))
	}
	if rules[0].PathRegex != `^personal[\\/]` {
		t.Errorf("path_regex[0] = %q", rules[0].PathRegex)
	}
	if len(rules[0].Age) != 2 {
		t.Errorf("получателей в personal = %d, ожидалось 2", len(rules[0].Age))
	}
	if len(rules[1].Age) != 2 {
		t.Errorf("получателей в правиле по умолчанию = %d, ожидалось 2", len(rules[1].Age))
	}
}

func TestRecipientsAreUnique(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	recipients := file.Recipients()
	if len(recipients) != 3 {
		t.Fatalf("получателей = %d, ожидалось 3: %v", len(recipients), recipients)
	}
	for _, recipient := range recipients {
		count := 0
		for _, other := range recipients {
			if other == recipient {
				count++
			}
		}
		if count != 1 {
			t.Errorf("получатель %s встречается %d раз", recipient, count)
		}
	}
}

func TestReplacePlaceholdersFillsKnownLabels(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	changed := file.ReplacePlaceholders(map[string]string{
		"PC":       "age1pc",
		"RECOVERY": "age1rec",
	})
	if !changed {
		t.Fatal("ожидалось изменение файла")
	}
	rules := file.Rules()
	if rules[0].Age[0] != "age1pc" {
		t.Errorf("подстановка не сработала: %v", rules[0].Age)
	}
	if rules[0].Age[1] != "age1rec" {
		t.Errorf("подстановка не сработала: %v", rules[0].Age)
	}
}

func TestReplacePlaceholdersIgnoresUnknownLabels(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Метка OTHER не совпадает ни с одной заглушкой: файл меняться не должен.
	if file.ReplacePlaceholders(map[string]string{"OTHER": "age1x"}) {
		t.Fatal("неизвестные метки не должны менять файл")
	}
	rules := file.Rules()
	if rules[0].Age[0] != "age1REPLACE_ME_PC" {
		t.Errorf("получатель изменён: %v", rules[0].Age)
	}
}

func TestReplacePlaceholdersLeavesRealKeys(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	file.ReplacePlaceholders(map[string]string{"PC": "age1pc"})
	rules := file.Rules()
	// Слот PC есть в обоих правилах: в обоих он и подменяется.
	if rules[0].Age[0] != "age1pc" || rules[1].Age[0] != "age1pc" {
		t.Errorf("ключ PC не подставлен в оба правила: %v", rules)
	}
	// Слот APIARION — отдельная роль, её ключ PC не заменяет.
	if rules[1].Age[1] != "age1REPLACE_ME_APIARION" {
		t.Errorf("слот APIARION затронут: %v", rules[1].Age)
	}
}

func TestReplacePlaceholdersIsCaseInsensitive(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !file.ReplacePlaceholders(map[string]string{"pc": "age1pc"}) {
		t.Fatal("ожидалось изменение файла")
	}
	rules := file.Rules()
	if rules[0].Age[0] != "age1pc" {
		t.Errorf("подстановка без учёта регистра не сработала: %v", rules[0].Age)
	}
}

func TestReplacePlaceholdersKeepsUnknownSlots(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Слот роли без ключа остаётся: его заполнит `adytum grant`.
	file.ReplacePlaceholders(map[string]string{"PC": "age1pc"})
	rules := file.Rules()
	if rules[0].Age[1] != "age1REPLACE_ME_RECOVERY" {
		t.Errorf("слот RECOVERY потерян: %v", rules[0].Age)
	}
	if rules[1].Age[1] != "age1REPLACE_ME_APIARION" {
		t.Errorf("слот APIARION потерян: %v", rules[1].Age)
	}
}

func TestUnresolvedPlaceholdersReportsMissingRoles(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	labels := file.UnresolvedPlaceholders(map[string]string{"PC": "age1pc"})
	if len(labels) != 2 {
		t.Fatalf("незаполненных меток = %d, ожидалось 2: %v", len(labels), labels)
	}
	found := map[string]bool{}
	for _, label := range labels {
		found[label] = true
	}
	if !found["RECOVERY"] || !found["APIARION"] {
		t.Errorf("незаполненные метки = %v, ожидались RECOVERY и APIARION", labels)
	}
}

func TestUnresolvedPlaceholdersEmptyWhenAllKnown(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	labels := file.UnresolvedPlaceholders(map[string]string{
		"PC":       "age1pc",
		"RECOVERY": "age1rec",
		"APIARION": "age1api",
	})
	if len(labels) != 0 {
		t.Fatalf("незаполненных меток = %v, ожидался пустой список", labels)
	}
}

func TestReplaceOrAddRecipientFillsPlaceholder(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	changed, err := file.ReplaceOrAddRecipient(`^personal[\\/]`, "age1pc", "PC")
	if err != nil {
		t.Fatalf("ReplaceOrAddRecipient: %v", err)
	}
	if !changed {
		t.Fatal("ожидалось изменение правила")
	}
	rules := file.Rules()
	if rules[0].Age[0] != "age1pc" {
		t.Errorf("заглушка не заменена: %v", rules[0].Age)
	}
	if len(rules[0].Age) != 2 {
		t.Errorf("количество получателей = %d, ожидалось 2", len(rules[0].Age))
	}
}

func TestReplaceOrAddRecipientAppendsWhenNoPlaceholder(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	changed, err := file.ReplaceOrAddRecipient(`^personal[\\/]`, "age1NEW", "NEW")
	if err != nil {
		t.Fatalf("ReplaceOrAddRecipient: %v", err)
	}
	if !changed {
		t.Fatal("ожидалось добавление получателя")
	}
	rules := file.Rules()
	if rules[0].Age[2] != "age1NEW" {
		t.Errorf("получатель не добавлен: %v", rules[0].Age)
	}
}

func TestAddRecipientAppendsToRule(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	added, err := file.AddRecipient(`^personal[\\/]`, "age1NEW")
	if err != nil {
		t.Fatalf("AddRecipient: %v", err)
	}
	if !added {
		t.Fatal("получатель не добавлен")
	}
	rules := file.Rules()
	if len(rules[0].Age) != 3 {
		t.Fatalf("получателей = %d, ожидалось 3", len(rules[0].Age))
	}
	if rules[0].Age[2] != "age1NEW" {
		t.Errorf("новый получатель = %q", rules[0].Age[2])
	}
}

func TestAddRecipientIsIdempotent(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	added, err := file.AddRecipient(`^personal[\\/]`, "age1REPLACE_ME_PC")
	if err != nil {
		t.Fatalf("AddRecipient: %v", err)
	}
	if added {
		t.Fatal("существующий получатель не должен добавляться повторно")
	}
}

func TestAddRecipientFailsOnUnknownRule(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := file.AddRecipient("^missing$", "age1NEW"); err == nil {
		t.Fatal("ожидалась ошибка для неизвестного правила")
	}
}

func TestRemoveRecipientRemovesEverywhere(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	removed := file.RemoveRecipient("age1REPLACE_ME_PC")
	if removed != 2 {
		t.Fatalf("удалено %d вхождений, ожидалось 2", removed)
	}
	for _, rule := range file.Rules() {
		for _, recipient := range rule.Age {
			if recipient == "age1REPLACE_ME_PC" {
				t.Fatalf("получатель age1REPLACE_ME_PC остался: %v", rule.Age)
			}
		}
	}
}

func TestBytesPreservesComments(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := file.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !strings.Contains(string(out), "комментарий сохраняется") {
		t.Errorf("комментарий потерян:\n%s", out)
	}
}

func TestBytesKeepsCreationRules(t *testing.T) {
	file, err := Parse([]byte(template))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out, err := file.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	reparsed, err := Parse(out)
	if err != nil {
		t.Fatalf("повторный Parse: %v", err)
	}
	if len(reparsed.Rules()) != 2 {
		t.Errorf("после сериализации правил = %d, ожидалось 2", len(reparsed.Rules()))
	}
}
