package adytum

import (
	"strings"
	"testing"
)

// TestSopsDecryptPassesContentThroughStdin проверяет, что содержимое
// файла передаётся sops через stdin.
//
// sops без имени файла читает ввод оттуда: без этого расшифровка
// всегда падала бы с «sops metadata not found».
func TestSopsDecryptPassesContentThroughStdin(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"decrypt": {out: "KEY=value\n"},
	}}
	sops := Sops{Runner: runner, Root: "/repo", KeyFile: "/keys/pc.txt"}

	out, err := sops.Decrypt("personal/github.env", []byte("sops_mac=abc\n"))
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(out) != "KEY=value\n" {
		t.Errorf("вывод = %q, ожидалось KEY=value", string(out))
	}
	if len(runner.calls) != 1 {
		t.Fatalf("вызовов = %d, ожидался 1", len(runner.calls))
	}
}

// TestSopsEncryptPassesContentThroughStdin проверяет то же для
// шифрования.
func TestSopsEncryptPassesContentThroughStdin(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"encrypt": {out: "KEY=ENC[...]\n"},
	}}
	sops := Sops{Runner: runner, Root: "/repo", KeyFile: "/keys/pc.txt"}

	out, err := sops.Encrypt("personal/github.env", []byte("KEY=value\n"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.Contains(string(out), "ENC[") {
		t.Errorf("вывод = %q, ожидался шифротекст", string(out))
	}
}

// TestSopsSetsAgeKeyFile проверяет, что sops получает ключ машины.
//
// Без SOPS_AGE_KEY_FILE sops не знает, чьим ключом расшифровывать, и
// расшифровка падает даже там, где доступ разрешён.
func TestSopsSetsAgeKeyFile(t *testing.T) {
	runner := &fakeRunner{responses: map[string]fakeResponse{
		"decrypt": {out: "KEY=value\n"},
	}}
	sops := Sops{Runner: runner, Root: "/repo", KeyFile: "/keys/pc.txt"}

	if _, err := sops.Decrypt("personal/github.env", []byte("data")); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	found := false
	for _, env := range runner.envs {
		for _, entry := range env {
			if strings.HasPrefix(entry, "SOPS_AGE_KEY_FILE=") {
				found = true
			}
		}
	}
	if !found {
		t.Error("SOPS_AGE_KEY_FILE не передан sops")
	}
}
