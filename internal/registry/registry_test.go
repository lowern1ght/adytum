package registry

import (
	"strings"
	"testing"

	"github.com/lowern1ght/adytum/internal/secretvalue"
)

// sampleRegistry — реестр со всеми полями.
const sampleRegistry = `version: 1

recipients:
  pc: age1pcpublickey
  apiarion: age1apiarionkey
  recovery: age1recoverykey

scopes:
  personal: [pc, recovery]
  default: [pc, apiarion, recovery]

entries:
  nova/vcenter:
    type: env
    description: vCenter API — креды terraform-провайдера
    scope: default
    rotates: https://vcenter.infra.hplatform.ai
    keys:
      VSPHERE_SERVER: hostname vCenter
      VSPHERE_USER: учётка
      VSPHERE_PASSWORD: пароль
  personal/github:
    type: env
    description: GitHub PAT
    scope: personal
    rotates: https://github.com/settings/tokens
`

func TestParseReadsScopesAndEntries(t *testing.T) {
	loaded, err := Parse([]byte(sampleRegistry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(loaded.Errors) > 0 {
		t.Fatalf("ошибки разбора: %v", loaded.Errors)
	}

	if loaded.Version != Version {
		t.Errorf("Version = %d, ожидалось %d", loaded.Version, Version)
	}
	if len(loaded.Scopes) != 2 {
		t.Fatalf("scopes = %d, ожидалось 2", len(loaded.Scopes))
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("entries = %d, ожидалось 2", len(loaded.Entries))
	}

	entry, found := loaded.Entry("nova/vcenter")
	if !found {
		t.Fatal("запись nova/vcenter не найдена")
	}
	if entry.Type != TypeEnv {
		t.Errorf("type = %q, ожидалось env", entry.Type)
	}
	if entry.Scope != "default" {
		t.Errorf("scope = %q, ожидалось default", entry.Scope)
	}
	if entry.Rotates != "https://vcenter.infra.hplatform.ai" {
		t.Errorf("rotates = %q", entry.Rotates)
	}
	if entry.Keys["VSPHERE_PASSWORD"] != "пароль" {
		t.Errorf("описание ключа = %q, ожидалось пароль", entry.Keys["VSPHERE_PASSWORD"])
	}
}

func TestParseKeepsGoingAfterBrokenEntry(t *testing.T) {
	broken := `version: 1
scopes:
  default: [pc]
entries:
  broken/one:
    type: nonsense
    description: неизвестный тип
    scope: default
  broken/two:
    description: без type
    scope: default
  good/one:
    type: env
    description: корректная запись
    scope: default
`

	loaded, err := Parse([]byte(broken))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(loaded.Errors) != 2 {
		t.Errorf("ошибок = %d, ожидалось 2: %v", len(loaded.Errors), loaded.Errors)
	}
	if _, found := loaded.Entry("good/one"); !found {
		t.Error("корректная запись потеряна из-за соседних битых")
	}
}

func TestParseReportsUnknownScope(t *testing.T) {
	content := `version: 1
scopes:
  default: [pc]
entries:
  nova/one:
    type: env
    description: запись
    scope: missing
`

	loaded, err := Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	unknown := loaded.UnknownScopes()
	if len(unknown) != 1 || unknown[0] != "missing" {
		t.Errorf("UnknownScopes = %v, ожидался [missing]", unknown)
	}
}

func TestParseReportsMissingRecipientKey(t *testing.T) {
	content := `version: 1
recipients:
  pc: age1pcpublickey
scopes:
  default: [pc, ghost]
entries:
  nova/one:
    type: env
    description: запись
    scope: default
`

	loaded, err := Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	unknown := loaded.UnknownMachines(nil)
	if len(unknown) != 1 || unknown[0] != "ghost" {
		t.Errorf("UnknownMachines = %v, ожидался [ghost]", unknown)
	}
}

func TestParseRejectsBrokenYaml(t *testing.T) {
	if _, err := Parse([]byte("version: [unclosed\n")); err == nil {
		t.Fatal("ожидалась ошибка разбора битого yaml")
	}
}

func TestParseRejectsUnsupportedVersion(t *testing.T) {
	loaded, err := Parse([]byte("version: 99\nscopes:\n  default: [pc]\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(loaded.Errors) == 0 {
		t.Error("неподдерживаемая версия схемы не отмечена")
	}
}

func TestGenerateSopsConfigMatchesScopes(t *testing.T) {
	loaded, err := Parse([]byte(sampleRegistry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	content, err := GenerateSopsConfig(loaded.Scopes, loaded.Recipients)
	if err != nil {
		t.Fatalf("GenerateSopsConfig: %v", err)
	}
	text := string(content)

	if !strings.Contains(text, "path_regex: '^personal[\\\\/]'") {
		t.Errorf("нет правила для personal:\n%s", text)
	}
	if !strings.Contains(text, "path_regex: '^.*$'") {
		t.Errorf("нет правила по умолчанию:\n%s", text)
	}
	// personal: pc + recovery — два ключа, apiarion в нём быть не должен.
	personal := ruleAge(text, "path_regex: '^personal[\\\\/]'")
	if len(personal) != 2 {
		t.Errorf("получателей personal = %d, ожидалось 2: %v", len(personal), personal)
	}
	for _, key := range personal {
		if key == "age1apiarionkey" {
			t.Error("apiarion попал в закрытый раздел personal")
		}
	}
	fallback := ruleAge(text, "path_regex: '^.*$'")
	if len(fallback) != 3 {
		t.Errorf("получателей default = %d, ожидалось 3: %v", len(fallback), fallback)
	}
}

func TestGenerateSopsConfigOrdersDefaultLast(t *testing.T) {
	loaded, err := Parse([]byte(sampleRegistry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	content, err := GenerateSopsConfig(loaded.Scopes, loaded.Recipients)
	if err != nil {
		t.Fatalf("GenerateSopsConfig: %v", err)
	}
	text := string(content)

	personal := strings.Index(text, "^personal")
	fallback := strings.Index(text, "^.*$")
	if personal < 0 || fallback < 0 {
		t.Fatalf("правила не найдены:\n%s", text)
	}
	if fallback < personal {
		t.Errorf("правило по умолчанию должно идти после частных:\n%s", text)
	}
}

func TestGenerateSopsConfigRequiresRecipientKeys(t *testing.T) {
	loaded, err := Parse([]byte(sampleRegistry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Ключи apiarion в реестре нет — собрать правила нечем.
	incomplete := map[string]string{"pc": "age1pcpublickey"}

	if _, err := GenerateSopsConfig(loaded.Scopes, incomplete); err == nil {
		t.Fatal("ожидалась ошибка отсутствующего ключа получателя")
	}
}

func TestSopsRulesMatchGeneratedConfig(t *testing.T) {
	loaded, err := Parse([]byte(sampleRegistry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rules, err := SopsRules(loaded.Scopes, loaded.Recipients)
	if err != nil {
		t.Fatalf("SopsRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("правил = %d, ожидалось 2", len(rules))
	}
	if len(rules[`^personal[\\/]`]) != 2 {
		t.Errorf("получателей personal = %d, ожидалось 2", len(rules[`^personal[\\/]`]))
	}
}

func TestResolveRecipientsPrefersRegistry(t *testing.T) {
	loaded, err := Parse([]byte(sampleRegistry))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	local := map[string]string{"pc": "age1local", "only-local": "age1onlylocal"}
	resolved := loaded.ResolveRecipients(local)

	if resolved["pc"] != "age1pcpublickey" {
		t.Errorf("pc = %q, ожидался ключ из реестра", resolved["pc"])
	}
	if resolved["only-local"] != "age1onlylocal" {
		t.Errorf("only-local = %q, ожидался локальный ключ", resolved["only-local"])
	}
}

// TestTypeFormatMapsKeyedTypes проверяет отображение типа секрета в
// формат файла.
//
// Формат нужен операциям над значениями: без отображения тип и формат
// расходились бы, и запись ключа ушла бы не в тот файл.
func TestTypeFormatMapsKeyedTypes(t *testing.T) {
	cases := map[Type]secretvalue.Format{
		TypeEnv:  secretvalue.FormatEnv,
		TypeYAML: secretvalue.FormatYAML,
		TypeJSON: secretvalue.FormatJSON,
	}
	for secretType, expected := range cases {
		if got := secretType.Format(); got != expected {
			t.Errorf("%s.Format() = %q, ожидалось %q", secretType, got, expected)
		}
	}
}

// TestTypeFormatIsEmptyForKeylessTypes проверяет, что типы без ключей
// формата не имеют.
func TestTypeFormatIsEmptyForKeylessTypes(t *testing.T) {
	for _, secretType := range []Type{TypeBinary, TypeKubeconfig} {
		if got := secretType.Format(); got != "" {
			t.Errorf("%s.Format() = %q, ожидалась пустая строка", secretType, got)
		}
	}
}

// ruleAge возвращает список age-ключей правила из текста .sops.yaml.
func ruleAge(text, marker string) []string {
	lines := strings.Split(text, "\n")
	var age []string
	inRule := false
	for _, line := range lines {
		if strings.Contains(line, marker) {
			inRule = true
			continue
		}
		if !inRule {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- age1") {
			if trimmed != "age:" && trimmed != "" {
				inRule = false
			}
			continue
		}
		age = append(age, strings.TrimPrefix(trimmed, "- "))
	}
	return age
}
