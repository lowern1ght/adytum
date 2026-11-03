// Package ui отвечает за вывод CLI: цвета, символы статуса и подсказки.
//
// Вывод различает три режима:
//
//   - обычный — цветной, если stdout это терминал и не задан NO_COLOR;
//   - --pretty — цветной всегда;
//   - --json — всегда чистый машинный вывод без декора.
//
// Символы Unicode ломают консоль Windows без UTF-8, поэтому при
// непригодной кодировке используется ASCII-фолбэк.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Style — оформление вывода.
type Style struct {
	// Color — включать ANSI-последовательности.
	Color bool
	// Unicode — использовать символы Unicode (✓/✗/⚠/🔒).
	Unicode bool
}

// Символы статусов в Unicode-варианте.
const (
	SymbolOK    = "✓"
	SymbolFail  = "✗"
	SymbolWarn  = "⚠"
	SymbolLock  = "🔒"
	SymbolArrow = "→"
)

// Символы статусов в ASCII-варианте.
const (
	AsciiOK    = "[ok]"
	AsciiFail  = "[x]"
	AsciiWarn  = "[!]"
	AsciiLock  = "[lock]"
	AsciiArrow = "->"
)

// ANSI-последовательности.
const (
	ansiReset  = "\033[0m"
	ansiGreen  = "\033[32m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiDim    = "\033[2m"
)

// Kind — вид символа статуса.
type Kind int

// Виды символов статуса.
const (
	// KindOK — успех.
	KindOK Kind = iota
	// KindFail — ошибка.
	KindFail
	// KindWarn — предупреждение.
	KindWarn
	// KindLock — заблокировано.
	KindLock
	// KindArrow — указатель на следующий шаг.
	KindArrow
)

// Printer печатает оформленный вывод.
type Printer struct {
	// Out — поток вывода.
	Out io.Writer
	// Style — оформление.
	Style Style
}

// New создаёт принтер для потока с учётом режима.
//
// pretty заставляет включить цвет, json отключает декор полностью.
func New(out io.Writer, pretty, jsonMode bool) *Printer {
	printer := &Printer{Out: out}
	if jsonMode {
		printer.Style = Style{Color: false, Unicode: true}
		return printer
	}
	color := pretty
	if !color {
		color = isTerminal(out) && os.Getenv("NO_COLOR") == ""
	}
	printer.Style = Style{Color: color, Unicode: supportsUnicode(out)}
	return printer
}

// OK печатает строку со знаком успеха.
func (p *Printer) OK(format string, args ...any) {
	p.line(p.Style.ok(), format, args...)
}

// Fail печатает строку со знаком ошибки.
func (p *Printer) Fail(format string, args ...any) {
	p.line(p.Style.fail(), format, args...)
}

// Warn печатает строку со знаком предупреждения.
func (p *Printer) Warn(format string, args ...any) {
	p.line(p.Style.warn(), format, args...)
}

// Info печатает строку с указателем направления.
func (p *Printer) Info(format string, args ...any) {
	p.line(p.Style.info(), format, args...)
}

// Lock печатает строку со знаком замка.
//
// Знак замка означает «нужно разрешение владельца».
func (p *Printer) Lock(format string, args ...any) {
	p.line(p.Style.lock(), format, args...)
}

// Plain печатает строку без оформления.
func (p *Printer) Plain(format string, args ...any) {
	fmt.Fprintf(p.Out, format+"\n", args...)
}

// Hint печатает подсказку следующего шага приглушённым цветом.
func (p *Printer) Hint(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	if !p.Style.Color {
		fmt.Fprintf(p.Out, "%s %s\n", p.Style.arrow(), text)
		return
	}
	fmt.Fprintf(p.Out, "%s%s %s%s\n", ansiDim, p.Style.arrow(), text, ansiReset)
}

// Mark возвращает символ статуса без оформления.
//
// Используется, когда символ нужен внутри текста: подсказки и
// сообщения об ошибках собираются в одну строку.
func (p *Printer) Mark(kind Kind) string {
	switch kind {
	case KindFail:
		return p.Style.symbol(SymbolFail, AsciiFail)
	case KindWarn:
		return p.Style.symbol(SymbolWarn, AsciiWarn)
	case KindLock:
		return p.Style.symbol(SymbolLock, AsciiLock)
	case KindArrow:
		return p.Style.symbol(SymbolArrow, AsciiArrow)
	default:
		return p.Style.symbol(SymbolOK, AsciiOK)
	}
}

// line печатает строку со знаком статуса.
func (p *Printer) line(symbol statusSymbol, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	if !p.Style.Color {
		fmt.Fprintf(p.Out, "%s %s\n", symbol.text, text)
		return
	}
	fmt.Fprintf(p.Out, "%s%s%s %s\n", symbol.color, symbol.text, ansiReset, text)
}

// statusSymbol — символ статуса вместе с его цветом.
type statusSymbol struct {
	color string
	text  string
}

// ok возвращает символ успеха.
func (s Style) ok() statusSymbol {
	return statusSymbol{color: ansiGreen, text: s.symbol(SymbolOK, AsciiOK)}
}

// fail возвращает символ ошибки.
func (s Style) fail() statusSymbol {
	return statusSymbol{color: ansiRed, text: s.symbol(SymbolFail, AsciiFail)}
}

// warn возвращает символ предупреждения.
func (s Style) warn() statusSymbol {
	return statusSymbol{color: ansiYellow, text: s.symbol(SymbolWarn, AsciiWarn)}
}

// info возвращает символ указателя.
func (s Style) info() statusSymbol {
	return statusSymbol{color: ansiBlue, text: s.symbol(SymbolArrow, AsciiArrow)}
}

// lock возвращает символ замка.
func (s Style) lock() statusSymbol {
	return statusSymbol{color: ansiYellow, text: s.symbol(SymbolLock, AsciiLock)}
}

// arrow возвращает символ указателя для подсказок.
func (s Style) arrow() string {
	return s.symbol(SymbolArrow, AsciiArrow)
}

// symbol выбирает Unicode или ASCII-вариант символа.
func (s Style) symbol(unicode, ascii string) string {
	if s.Unicode {
		return unicode
	}
	return ascii
}

// isTerminal сообщает, является ли поток терминалом.
//
// Проверка через ModeCharDevice работает и на Unix, и на Windows и не
// требует внешних зависимостей.
func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// supportsUnicode сообщает, умеет ли консоль печатать Unicode.
//
// На Windows проверяется кодовая страница вывода: 65001 означает UTF-8.
// В остальных случаях считаем, что умеет.
func supportsUnicode(out io.Writer) bool {
	return consoleSupportsUnicode(out)
}

// WrapIndent добавляет отступ к каждой строке текста.
//
// Используется для блоков подсказок, чтобы они читались как продолжение
// сообщения.
func WrapIndent(text string, indent string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if line == "" {
			continue
		}
		lines[index] = indent + line
	}
	return strings.Join(lines, "\n")
}
