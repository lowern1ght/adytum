package adytum

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/lowern1ght/adytum/internal/envfile"
	"github.com/lowern1ght/adytum/internal/registry"
	"github.com/lowern1ght/adytum/internal/secretvalue"
)

// ListKeysReport — результат команды list --keys.
type ListKeysReport struct {
	// Name — путь секрета в хранилище.
	Name string `json:"name"`
	// Type — тип секрета.
	Type string `json:"type"`
	// Keys — имена ключей секрета, без значений.
	Keys []string `json:"keys"`
	// Size — размер файла секрета в байтах (для типов без ключей).
	Size int64 `json:"size,omitempty"`
	// Mime — тип содержимого файла (для типов без ключей).
	Mime string `json:"mime,omitempty"`
}

// ListKeys печатает имена ключей секрета.
//
// Значения не печатаются: команда отвечает на вопрос «какие ключи
// есть», а не «что в них лежит». Секрет расшифровывается в памяти,
// наружу уходят только имена.
//
// Состав берётся из самого файла, а не из реестра: реестр описывает
// ключи, которые владелец объявил, и он может расходиться с файлом.
//
// Расшифровка требует активной разблокировки: это операция того же
// класса, что get и run.
func (a *App) ListKeys(entry string) error {
	if entry == "" {
		return fmt.Errorf("%w: укажите запись: adytum list <entry> --keys", ErrUsage)
	}
	if err := a.requireUnlock(); err != nil {
		return err
	}
	loaded, err := a.loadRegistry()
	if err != nil {
		return a.registryError(err)
	}
	record, found := loaded.Entry(entry)
	if !found {
		return fmt.Errorf("%w: запись %s не найдена в реестре", ErrNotFound, entry)
	}
	path, err := a.secretPath(entry, record.Type)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return a.missingSecretError(entry, "")
	}
	if err := a.requireInScope(path); err != nil {
		return err
	}
	plain, err := a.readPlaintext(path)
	if err != nil {
		return err
	}
	report, err := listKeysReport(entry, record.Type, plain)
	if err != nil {
		return err
	}
	return a.printListKeys(report)
}

// listKeysReport собирает отчёт об именах ключей секрета.
//
// Тип без ключей (binary, kubeconfig) описывается размером файла и
// типом содержимого: читать такой секрет можно только целиком.
func listKeysReport(entry string, secretType registry.Type, plain []byte) (ListKeysReport, error) {
	report := ListKeysReport{
		Name: entry,
		Type: string(secretType),
		Keys: []string{},
	}
	if !secretType.Keyed() {
		report.Size = int64(len(plain))
		report.Mime = http.DetectContentType(plain)
		return report, nil
	}
	names, err := secretKeyNames(secretType, plain)
	if err != nil {
		return ListKeysReport{}, err
	}
	report.Keys = names
	return report, nil
}

// secretKeyNames возвращает имена ключей секрета по его типу.
//
// Формат определяет состав: env разбирается построчно, и ключи идут в
// порядке файла; yaml и json — по ключам верхнего уровня.
//
// Текст ошибки разбора не показывается: парсер yaml цитирует то, что
// не смог разобрать, а это может быть фрагмент секрета.
func secretKeyNames(secretType registry.Type, plain []byte) ([]string, error) {
	if secretType == registry.TypeEnv {
		entries, err := envfile.Parse(plain)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Key)
		}
		return names, nil
	}
	names, err := secretvalue.Keys(secretType.Format(), plain)
	if err != nil {
		return nil, fmt.Errorf("не удалось разобрать документ типа %s: формат файла не соответствует типу секрета", secretType)
	}
	return names, nil
}

// printListKeys печатает имена ключей секрета.
//
// Значений в выводе нет: ни в stdout, ни в подсказках.
func (a *App) printListKeys(report ListKeysReport) error {
	if a.JSON {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		a.println(string(encoded))
		return nil
	}

	printer := a.ui()
	header := fmt.Sprintf("%s — %s", report.Name, pluralKeys(len(report.Keys)))
	if report.Size > 0 {
		header = fmt.Sprintf("%s, файл %d байт (%s)", header, report.Size, report.Mime)
	}
	if len(report.Keys) == 0 {
		printer.Plain("%s", header)
		return nil
	}
	printer.Plain("%s:", header)
	for _, name := range report.Keys {
		printer.Plain("  %s", name)
	}
	return nil
}

// pluralKeys возвращает подпись количества ключей.
//
// Форма русского слова зависит от числа: 1 ключ, 2 ключа, 5 ключей.
func pluralKeys(count int) string {
	if count == 0 {
		return "ключей нет"
	}
	remainder := count % 100
	if remainder >= 11 && remainder <= 14 {
		return fmt.Sprintf("%d ключей", count)
	}
	switch count % 10 {
	case 1:
		return fmt.Sprintf("%d ключ", count)
	case 2, 3, 4:
		return fmt.Sprintf("%d ключа", count)
	default:
		return fmt.Sprintf("%d ключей", count)
	}
}
