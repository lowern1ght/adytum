//go:build !windows

package ui

import "io"

// consoleSupportsUnicode сообщает, умеет ли консоль печатать Unicode.
//
// На Unix кодировка терминала не определяется: терминалы, не умеющие
// UTF-8, встречаются редко, а ломать вывод на пустом месте хуже.
func consoleSupportsUnicode(out io.Writer) bool {
	return true
}
