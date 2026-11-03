package ui

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrinterNoColorWithoutTerminal проверяет, что цвет не включается
// на не-терминале.
//
// Цветной вывод в перенаправленном потоке ломает разбор: агенты и
// скрипты читают вывод как данные.
func TestPrinterNoColorWithoutTerminal(t *testing.T) {
	var out bytes.Buffer

	printer := New(&out, false, false)

	if printer.Style.Color {
		t.Error("цвет включён без терминала")
	}
}

// TestPrinterPrettyForcesColor проверяет, что --pretty включает цвет
// принудительно.
func TestPrinterPrettyForcesColor(t *testing.T) {
	var out bytes.Buffer

	printer := New(&out, true, false)

	if !printer.Style.Color {
		t.Error("флаг --pretty не включил цвет")
	}
}

// TestPrinterJSONDisablesColor проверяет, что машинный режим не
// содержит ANSI-последовательностей.
func TestPrinterJSONDisablesColor(t *testing.T) {
	var out bytes.Buffer

	printer := New(&out, true, true)

	if printer.Style.Color {
		t.Error("цвет включён в машинном режиме")
	}
}

// TestPrinterMarksUseUnicodeOrAscii проверяет выбор символа.
//
// Символы Unicode ломают консоль Windows без UTF-8: там нужен
// ASCII-фолбэк.
func TestPrinterMarksUseUnicodeOrAscii(t *testing.T) {
	var out bytes.Buffer

	printer := New(&out, false, false)
	printer.Style.Unicode = true
	if printer.Mark(KindOK) != SymbolOK {
		t.Errorf("Mark(KindOK) = %q, ожидалось %q", printer.Mark(KindOK), SymbolOK)
	}

	printer.Style.Unicode = false
	if printer.Mark(KindOK) != AsciiOK {
		t.Errorf("Mark(KindOK) = %q, ожидалось %q", printer.Mark(KindOK), AsciiOK)
	}
	if printer.Mark(KindLock) != AsciiLock {
		t.Errorf("Mark(KindLock) = %q, ожидалось %q", printer.Mark(KindLock), AsciiLock)
	}
}

// TestPrinterHintUsesArrow проверяет, что подсказка помечена стрелкой.
func TestPrinterHintUsesArrow(t *testing.T) {
	var out bytes.Buffer

	printer := New(&out, false, false)
	printer.Style.Unicode = false
	printer.Hint("сделай следующий шаг")

	if !strings.Contains(out.String(), AsciiArrow) {
		t.Errorf("вывод = %q, ожидалась стрелка", out.String())
	}
}
