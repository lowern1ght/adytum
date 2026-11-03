//go:build windows

package ui

import (
	"io"
	"os"
	"syscall"
)

// consoleSupportsUnicode сообщает, умеет ли консоль Windows печатать Unicode.
//
// Проверяется кодовая страница вывода консоли: 65001 — это UTF-8.
// Если вывод перенаправлен в файл или пайп, кодировка не важна.
func consoleSupportsUnicode(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return true
	}
	info, err := file.Stat()
	if err != nil {
		return true
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		// Вывод перенаправлен: байты не проходят через кодовую страницу.
		return true
	}
	code, ok := consoleOutputCodePage(file.Fd())
	if !ok {
		return false
	}
	return code == 65001
}

// consoleOutputCodePage возвращает кодовую страницу вывода консоли.
func consoleOutputCodePage(handle uintptr) (uint32, bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetConsoleOutputCP")
	code, _, err := proc.Call()
	if err != nil {
		return 0, false
	}
	return uint32(code), true
}
