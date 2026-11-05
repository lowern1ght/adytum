package adytum

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStatusJSONReportsLockedState проверяет, что статус отдаёт
// машиночитаемое состояние гейта.
//
// Автоматике нужно отличить «закрыто, жди владельца» от «сломано»:
// отдельное поле и код возврата дают это различие.
func TestStatusJSONReportsLockedState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	app := newTestApp(t, &fakeRunner{}, t.TempDir())
	app.JSON = true

	var out strings.Builder
	app.Out = &out
	if err := app.StatusJSON(); err != nil {
		t.Fatalf("StatusJSON: %v", err)
	}

	var report StatusReport
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatalf("JSON не разбирается: %v", err)
	}
	if !report.Locked {
		t.Error("locked = false, ожидалось true: маркера нет")
	}
	if report.Reason != "locked" {
		t.Errorf("reason = %q, ожидалось locked", report.Reason)
	}
	if report.Unlock.TTL == "" {
		t.Error("в отчёте нет срока жизни маркера")
	}
}

// TestStatusJSONReportsBrokenMarker проверяет различение сломанного
// маркера и отсутствующего.
//
// Сломанный маркер — проблема, отсутствующий — штатное состояние: по
// одному полю automation не поймёт, что чинить.
func TestStatusJSONReportsBrokenMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".adytum"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".adytum", "unlocked"), []byte("не метка\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := newTestApp(t, &fakeRunner{}, t.TempDir())

	report := app.statusReport()
	if !report.Locked {
		t.Error("locked = false, ожидалось true")
	}
	if report.Reason != "broken" {
		t.Errorf("reason = %q, ожидалось broken", report.Reason)
	}
}

// TestStatusJSONReportsActiveMarker проверяет состояние активного
// маркера.
func TestStatusJSONReportsActiveMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".adytum"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".adytum", "unlocked"), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := newTestApp(t, &fakeRunner{}, t.TempDir())

	report := app.statusReport()
	if report.Locked {
		t.Error("locked = true, ожидалось false")
	}
	if !report.Unlock.Active {
		t.Error("маркер не активен")
	}
	if report.Unlock.ExpiresAt == "" {
		t.Error("в отчёте нет срока истечения")
	}
}
