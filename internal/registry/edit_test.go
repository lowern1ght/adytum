package registry

import (
	"strings"
	"testing"
)

// scopeLine возвращает строку раздела scopes реестра.
//
// Проверка идёт по разделу scopes: метка машины встречается ещё и в
// recipients, и без разделения проверка ничего не значила бы.
func scopeLine(t *testing.T, document *Document) string {
	t.Helper()
	content, err := document.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "personal:") {
			return line
		}
	}
	t.Fatalf("scope personal не найден:\n%s", string(content))
	return ""
}

// TestRemoveFromScopeKeepsEmptyScope проверяет, что отзыв последней
// машины не удаляет scope из реестра.
//
// Удалённый scope сделал бы записи unknown-scope: следующая
// перешифровка положила бы их файлы под правило по умолчанию, то есть
// отдала бы чужим получателям. Пустой scope виден в doctor и
// остаётся местом, куда выдают доступ.
func TestRemoveFromScopeKeepsEmptyScope(t *testing.T) {
	document, err := ParseDocument([]byte(`version: 1

recipients:
  pc: age1pcpublickey

scopes:
  personal: [pc]

entries:
  personal/github:
    type: env
    description: GitHub PAT
    scope: personal
`))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	document.RemoveFromScope("personal", "pc")

	line := scopeLine(t, document)
	if !strings.Contains(line, "personal:") {
		t.Errorf("scope personal удалён из реестра: %q", line)
	}
	if strings.Contains(line, "pc") {
		t.Errorf("машина pc осталась в scope: %q", line)
	}
}

// TestRemoveFromScopeKeepsOtherMachines проверяет, что отзыв одной
// машины не трогает остальных.
func TestRemoveFromScopeKeepsOtherMachines(t *testing.T) {
	document, err := ParseDocument([]byte(`version: 1

recipients:
  pc: age1pcpublickey
  recovery: age1recoverykey

scopes:
  personal: [pc, recovery]
`))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	document.RemoveFromScope("personal", "pc")

	line := scopeLine(t, document)
	if !strings.Contains(line, "recovery") {
		t.Errorf("машина recovery потеряна: %q", line)
	}
	if strings.Contains(line, "pc") {
		t.Errorf("машина pc не удалена: %q", line)
	}
}
