package adytum

import (
	"errors"
	"strings"

	"github.com/lowern1ght/adytum/internal/ui"
)

// Advice — подсказка «что делать дальше».
//
// Подсказка нужна не столько человеку (он и так знает), сколько
// агентской сессии: агент видит ошибку и должен либо исправить её сам,
// либо попросить владельца. Без подсказки агент обычно застревает.
type Advice struct {
	// Icon — вид значка: замок, стрелка или предупреждение.
	Icon ui.Kind
	// Text — текст подсказки.
	Text string
}

// Adviser — ошибка, умеющая подсказать следующий шаг.
type Adviser interface {
	error
	// Advice возвращает подсказку для ошибки.
	Advice() Advice
}

// AdviceError — ошибка с подсказкой следующего шага.
type AdviceError struct {
	// Err — исходная ошибка.
	Err error
	// Hint — подсказка.
	Hint Advice
}

// Error возвращает текст исходной ошибки.
func (e *AdviceError) Error() string {
	return e.Err.Error()
}

// Unwrap возвращает исходную ошибку.
func (e *AdviceError) Unwrap() error {
	return e.Err
}

// Advice возвращает подсказку.
func (e *AdviceError) Advice() Advice {
	return e.Hint
}

// advise оборачивает ошибку подсказкой.
func advise(err error, icon ui.Kind, text string) error {
	return &AdviceError{Err: err, Hint: Advice{Icon: icon, Text: text}}
}

// Тексты подсказок. Формулировки одинаковы в CLI и в плагине:
// агент видит одну и ту же подсказку независимо от точки входа.
const (
	// AdviceUnlock — операция закрыта маркером разблокировки.
	AdviceUnlock = "попроси владельца: adytum unlock (TTL 8h)"
	// AdviceRemote — remote не настроен.
	AdviceRemote = "adytum bootstrap-remote"
	// AdviceNotRecipient — машина не в списке получателей.
	AdviceNotRecipient = "секция недоступна этой машине (recipients .sops.yaml) — ожидаемо"
)

// ErrUnlockRequired — операция требует активного маркера разблокировки.
var ErrUnlockRequired = errors.New("операция закрыта: нет активной разблокировки")

// ErrDoctorFailed — самопроверка нашла проблемы.
//
// Отдельная ошибка, а не текст: подробности уже напечатаны в отчёте, и
// повторять их в stderr не нужно.
var ErrDoctorFailed = errors.New("проверки не пройдены")

// unlockError возвращает ошибку с подсказкой про разблокировку.
func unlockError() error {
	return advise(ErrUnlockRequired, ui.KindLock, AdviceUnlock)
}

// remoteError возвращает ошибку с подсказкой про bootstrap-remote.
func remoteError() error {
	return advise(errors.New("remote не настроен"), ui.KindArrow, AdviceRemote)
}

// notRecipientError возвращает ошибку с подсказкой про получателей.
//
// Отсутствие доступа к разделу — ожидаемое состояние машины, а не
// поломка: ошибка сообщает это прямо, чтобы агент не искал обходной
// путь и не пытался чинить шифрование.
func notRecipientError(err error) error {
	return advise(err, ui.KindWarn, AdviceNotRecipient)
}

// isNotRecipient сообщает, что ошибка вызвана отсутствием доступа.
//
// sops сообщает о невозможности расшифровки двумя способами: общей
// фразой про ключ данных и подробным перечислением получателей.
func isNotRecipient(message string) bool {
	markers := []string{
		"no identity matched any of the recipients",
		"Failed to get the data key",
		"does not match any of the recipients",
	}
	lowered := strings.ToLower(message)
	for _, marker := range markers {
		if strings.Contains(lowered, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}
