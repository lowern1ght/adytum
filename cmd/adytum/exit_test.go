package main

import (
	"fmt"
	"testing"

	"github.com/lowern1ght/adytum/internal/adytum"
)

// TestExitCodeForLockedState проверяет, что закрытый гейт
// разблокировки даёт отдельный код возврата.
//
// Гейт закрыт — это состояние, а не поломка: команда не выполнилась
// по понятной причине. Свой код позволяет автоматике ждать владельца,
// а не искать обходной путь.
func TestExitCodeForLockedState(t *testing.T) {
	err := fmt.Errorf("обёртка: %w", adytum.ErrUnlockRequired)

	if code := exitCodeFor(err); code != exitLocked {
		t.Errorf("код возврата = %d, ожидался %d", code, exitLocked)
	}
}

// TestExitCodeForPlainError проверяет, что обычная ошибка даёт код 1.
func TestExitCodeForPlainError(t *testing.T) {
	err := fmt.Errorf("обычная ошибка")

	if code := exitCodeFor(err); code != exitError {
		t.Errorf("код возврата = %d, ожидался %d", code, exitError)
	}
}
