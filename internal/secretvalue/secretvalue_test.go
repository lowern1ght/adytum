package secretvalue

import (
	"strings"
	"testing"
)

// TestSetEnvRejectsMultilineValue проверяет, что многострочное
// значение отклоняется для формата env.
//
// Файл env читается построчно: перенос строки внутри значения разорвал
// бы запись на две, и парсер упал бы с «ожидалась пара KEY=VALUE».
// Значение достать было бы нельзя — ни агенту, ни владельцу.
func TestSetEnvRejectsMultilineValue(t *testing.T) {
	cases := map[string]string{
		"перевод строки":  "line1\nline2",
		"возврат каретки": "line1\r\nline2",
	}
	for name, value := range cases {
		_, err := Set(FormatEnv, []byte("TOKEN=old\n"), "TOKEN", value)
		if err == nil {
			t.Errorf("%s: ожидался отказ", name)
			continue
		}
		if !strings.Contains(err.Error(), "yaml") {
			t.Errorf("%s: ошибка = %q, ожидалось упоминание типа yaml", name, err.Error())
		}
	}
}

// TestSetEnvKeepsSingleLineValue проверяет, что однострочное значение
// пишется как прежде.
func TestSetEnvKeepsSingleLineValue(t *testing.T) {
	content, err := Set(FormatEnv, []byte("TOKEN=old\n"), "TOKEN", "new")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !strings.Contains(string(content), "TOKEN=new") {
		t.Errorf("значение не записано: %q", string(content))
	}
}

// TestSetYAMLWritesMultilineValue проверяет, что yaml умеет хранить
// многострочное значение.
//
// Многострочные секреты (PEM-ключи) должны помещаться в yaml: формат
// для них и предназначен.
func TestSetYAMLWritesMultilineValue(t *testing.T) {
	value := "-----BEGIN KEY-----\nabc\ndef\n-----END KEY-----\n"

	content, err := Set(FormatYAML, nil, "KEY", value)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, found, err := Get(FormatYAML, content, "KEY")
	if err != nil || !found {
		t.Fatalf("Get: %v found=%v", err, found)
	}
	if got != value {
		t.Errorf("значение = %q, ожидалось %q", got, value)
	}
}

// TestSetJSONWritesMultilineValue проверяет, что json умеет хранить
// многострочное значение.
func TestSetJSONWritesMultilineValue(t *testing.T) {
	value := "-----BEGIN KEY-----\nabc\ndef\n-----END KEY-----\n"

	content, err := Set(FormatJSON, nil, "KEY", value)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, found, err := Get(FormatJSON, content, "KEY")
	if err != nil || !found {
		t.Fatalf("Get: %v found=%v", err, found)
	}
	if got != value {
		t.Errorf("значение = %q, ожидалось %q", got, value)
	}
}
