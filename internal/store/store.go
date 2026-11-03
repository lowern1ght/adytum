// Package store хранит локальное состояние adytum: каталог ~/.adytum,
// файлы идентичностей age, реестр получателей и маркер разблокировки.
package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DirName — каталог локального состояния внутри домашнего каталога.
	DirName = ".adytum"
	// KeysDirName — подкаталог с приватными идентичностями age.
	KeysDirName = "keys"
	// RecipientsFileName — реестр «метка=публичный ключ».
	RecipientsFileName = "recipients"
	// IdentityFileName — файл с меткой текущей машины.
	IdentityFileName = "identity"
	// UnlockFileName — маркер разблокировки для агентских сессий.
	UnlockFileName = "unlocked"
	// UnlockTTL — срок жизни маркера разблокировки.
	UnlockTTL = 8 * time.Hour
)

// Home возвращает путь к каталогу состояния (~/.adytum).
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить домашний каталог: %w", err)
	}
	return filepath.Join(home, DirName), nil
}

// KeysDir возвращает путь к каталогу идентичностей.
func KeysDir() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, KeysDirName), nil
}

// KeyFile возвращает путь к приватной идентичности метки.
func KeyFile(label string) (string, error) {
	dir, err := KeysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, label+".txt"), nil
}

// RecipientsFile возвращает путь к реестру получателей.
func RecipientsFile() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, RecipientsFileName), nil
}

// IdentityPath возвращает путь к файлу с меткой текущей машины.
func IdentityPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, IdentityFileName), nil
}

// UnlockPath возвращает путь к маркеру разблокировки.
func UnlockPath() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, UnlockFileName), nil
}

// EnsureHome создаёт каталог состояния и подкаталог идентичностей.
func EnsureHome() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(home, KeysDirName), 0o700); err != nil {
		return "", err
	}
	return home, nil
}

// LoadRecipients читает реестр «метка=age1...».
func LoadRecipients() (map[string]string, error) {
	path, err := RecipientsFile()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	recipients := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		label, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		recipients[strings.TrimSpace(label)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return recipients, nil
}

// SaveRecipients перезаписывает реестр получателей.
func SaveRecipients(recipients map[string]string) error {
	path, err := RecipientsFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var builder strings.Builder
	builder.WriteString("# метка=публичный ключ age\n")
	for _, label := range sortedLabels(recipients) {
		builder.WriteString(label + "=" + recipients[label] + "\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

// IdentityLabel возвращает метку идентичности текущей машины.
func IdentityLabel() (string, error) {
	path, err := IdentityPath()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("метка машины не задана: создайте %s", path)
		}
		return "", err
	}
	label := strings.TrimSpace(string(content))
	if label == "" {
		return "", fmt.Errorf("метка машины пуста: %s", path)
	}
	return label, nil
}

// SetIdentityLabel сохраняет метку идентичности текущей машины.
func SetIdentityLabel(label string) error {
	path, err := IdentityPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(label+"\n"), 0o600)
}

// IdentityFile возвращает путь к приватной идентичности текущей машины.
func IdentityFile() (string, error) {
	label, err := IdentityLabel()
	if err != nil {
		return "", err
	}
	return KeyFile(label)
}

// PublicKeyFromIdentity извлекает публичный ключ из файла идентичности age.
// Формат age-keygen: строки-комментарии, затем AGE-SECRET-KEY-1...
func PublicKeyFromIdentity(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") && strings.Contains(line, "public key:") {
			_, value, found := strings.Cut(line, "public key:")
			if found {
				return strings.TrimSpace(value), nil
			}
		}
	}
	return "", fmt.Errorf("в %s не найден публичный ключ", path)
}

// UnlockState описывает состояние маркера разблокировки.
type UnlockState struct {
	// Unlocked — маркер существует и не истёк.
	Unlocked bool
	// ExpiresAt — момент истечения (только если маркер существует).
	ExpiresAt time.Time
	// Remaining — оставшееся время (только если маркер действителен).
	Remaining time.Duration
}

// Unlock читает состояние маркера разблокировки.
func Unlock(now time.Time) (UnlockState, error) {
	path, err := UnlockPath()
	if err != nil {
		return UnlockState{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UnlockState{}, nil
		}
		return UnlockState{}, err
	}
	stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(string(content)))
	if err != nil {
		return UnlockState{}, nil
	}
	expires := stamp.Add(UnlockTTL)
	remaining := expires.Sub(now)
	return UnlockState{
		Unlocked:  remaining > 0,
		ExpiresAt: expires,
		Remaining: remaining,
	}, nil
}

// WriteUnlock создаёт (или обновляет) маркер разблокировки.
func WriteUnlock(now time.Time) (time.Time, error) {
	path, err := UnlockPath()
	if err != nil {
		return time.Time{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return time.Time{}, err
	}
	if err := os.WriteFile(path, []byte(now.Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return time.Time{}, err
	}
	return now.Add(UnlockTTL), nil
}

// sortedLabels возвращает метки в алфавитном порядке.
func sortedLabels(recipients map[string]string) []string {
	labels := make([]string, 0, len(recipients))
	for label := range recipients {
		labels = append(labels, label)
	}
	for i := 1; i < len(labels); i++ {
		for j := i; j > 0 && labels[j] < labels[j-1]; j-- {
			labels[j], labels[j-1] = labels[j-1], labels[j]
		}
	}
	return labels
}
