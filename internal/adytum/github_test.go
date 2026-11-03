package adytum

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newGitHubServer поднимает подделку API GitHub.
//
// Ответы задаются обработчиком: проверка идёт по методу и пути.
func newGitHubServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// TestCurrentUserReadsLogin проверяет определение владельца по токену.
func TestCurrentUserReadsLogin(t *testing.T) {
	server := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("путь = %q, ожидался /user", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("заголовок Authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "owner"})
	})

	client := &GitHubClient{BaseURL: server.URL}

	login, err := client.CurrentUser(context.Background(), "secret-token")
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if login != "owner" {
		t.Errorf("login = %q, ожидалось owner", login)
	}
}

// TestCreateRepoRequestsPrivateRepo проверяет параметры создания
// репозитория данных.
//
// Имя фиксировано, а приватность обязательна: публичный репозиторий с
// шифротекстом всё равно раскрывает структуру хранилища.
func TestCreateRepoRequestsPrivateRepo(t *testing.T) {
	server := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("метод = %q, ожидался POST", r.Method)
		}
		if r.URL.Path != "/user/repos" {
			t.Errorf("путь = %q, ожидался /user/repos", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("тело запроса: %v", err)
		}
		if payload["name"] != SecretsRepo {
			t.Errorf("имя = %v, ожидалось %q", payload["name"], SecretsRepo)
		}
		if payload["private"] != true {
			t.Errorf("private = %v, ожидалось true", payload["private"])
		}
		w.WriteHeader(http.StatusCreated)
	})

	client := &GitHubClient{BaseURL: server.URL}

	created, err := client.CreateRepo(context.Background(), "secret-token", SecretsRepo)
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if !created {
		t.Error("created = false, ожидалось true")
	}
}

// TestCreateRepoAcceptsExistingRepo проверяет, что уже существующий
// репозиторий не считается ошибкой.
//
// Повторный bootstrap-remote должен быть безопасен: 422 означает, что
// репозиторий уже есть.
func TestCreateRepoAcceptsExistingRepo(t *testing.T) {
	server := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	client := &GitHubClient{BaseURL: server.URL}

	created, err := client.CreateRepo(context.Background(), "secret-token", SecretsRepo)
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if created {
		t.Error("created = true, ожидалось false для существующего репозитория")
	}
}

// TestCurrentUserReportsFailure проверяет, что отказ API не остаётся
// молчаливым.
func TestCurrentUserReportsFailure(t *testing.T) {
	server := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	client := &GitHubClient{BaseURL: server.URL}

	if _, err := client.CurrentUser(context.Background(), "bad-token"); err == nil {
		t.Fatal("ожидалась ошибка при отказе авторизации")
	}
}
