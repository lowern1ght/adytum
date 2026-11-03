package envfile

import "testing"

func TestParseReadsKeyValuePairs(t *testing.T) {
	entries, err := Parse([]byte("KEY=value\nOTHER=second\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ожидалось 2 записи, получено %d", len(entries))
	}
	if entries[0].Key != "KEY" || entries[0].Value != "value" {
		t.Errorf("первая запись = %+v, ожидалось KEY=value", entries[0])
	}
	if entries[1].Key != "OTHER" || entries[1].Value != "second" {
		t.Errorf("вторая запись = %+v, ожидалось OTHER=second", entries[1])
	}
}

func TestParseSkipsCommentsAndBlankLines(t *testing.T) {
	content := "# комментарий\n\nKEY=value\n   \n#Another=ignored\n"
	entries, err := Parse([]byte(content))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ожидалась 1 запись, получено %d: %+v", len(entries), entries)
	}
	if entries[0].Key != "KEY" {
		t.Errorf("ключ = %q, ожидалось KEY", entries[0].Key)
	}
}

func TestParseAcceptsExportPrefix(t *testing.T) {
	entries, err := Parse([]byte("export KEY=value\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "KEY" {
		t.Fatalf("записи = %+v, ожидалась KEY=value", entries)
	}
}

func TestParseStripsQuotes(t *testing.T) {
	cases := map[string]string{
		`KEY="value"`:       "value",
		`KEY='value'`:       "value",
		`KEY=value`:         "value",
		`KEY="a b"`:         "a b",
		`KEY="line\nbreak"`: "line\nbreak",
	}
	for input, want := range cases {
		entries, err := Parse([]byte(input + "\n"))
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if len(entries) != 1 {
			t.Fatalf("Parse(%q): ожидалась 1 запись, получено %d", input, len(entries))
		}
		if entries[0].Value != want {
			t.Errorf("Parse(%q) = %q, ожидалось %q", input, entries[0].Value, want)
		}
	}
}

func TestParseKeepsEqualsInsideValue(t *testing.T) {
	entries, err := Parse([]byte("KEY=a=b=c\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if entries[0].Value != "a=b=c" {
		t.Errorf("значение = %q, ожидалось a=b=c", entries[0].Value)
	}
}

func TestParseRejectsMalformedLines(t *testing.T) {
	if _, err := Parse([]byte("NOT_A_PAIR\n")); err == nil {
		t.Fatal("ожидалась ошибка для строки без '='")
	}
	if _, err := Parse([]byte("=value\n")); err == nil {
		t.Fatal("ожидалась ошибка для пустого ключа")
	}
}

func TestParseTrimsSpacesAroundKey(t *testing.T) {
	entries, err := Parse([]byte("  KEY  =  value  \n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if entries[0].Key != "KEY" || entries[0].Value != "value" {
		t.Errorf("запись = %+v, ожидалось KEY=value", entries[0])
	}
}

func TestLookupFindsValue(t *testing.T) {
	entries, err := Parse([]byte("FIRST=1\nSECOND=2\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	value, found := Lookup(entries, "SECOND")
	if !found {
		t.Fatal("ключ SECOND не найден")
	}
	if value != "2" {
		t.Errorf("значение = %q, ожидалось 2", value)
	}
}

func TestLookupReportsMissingKey(t *testing.T) {
	entries, err := Parse([]byte("FIRST=1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, found := Lookup(entries, "MISSING"); found {
		t.Fatal("ключ MISSING не должен быть найден")
	}
}

func TestLookupLastDuplicateWins(t *testing.T) {
	entries, err := Parse([]byte("KEY=first\nKEY=second\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	value, found := Lookup(entries, "KEY")
	if !found {
		t.Fatal("ключ KEY не найден")
	}
	if value != "second" {
		t.Errorf("значение = %q, ожидалось second", value)
	}
}
