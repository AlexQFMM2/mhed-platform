package mailservice

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
)

func testService(t *testing.T) *Service {
	t.Helper()
	service, err := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)), []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestSealRoundTripDoesNotExposePlaintext(t *testing.T) {
	service := testService(t)
	plain := []byte(`{"code":"123456","username":"hunter"}`)
	ciphertext, nonce, err := service.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) == string(plain) {
		t.Fatal("ciphertext contains plaintext")
	}
	opened, err := service.Open(ciphertext, nonce)
	if err != nil || string(opened) != string(plain) {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestRandomCodeIsSixDigits(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9]{6}$`)
	for index := 0; index < 32; index++ {
		code, err := RandomCode()
		if err != nil || !pattern.MatchString(code) {
			t.Fatalf("invalid code %q: %v", code, err)
		}
	}
}

func TestProviderAcceptsNumericAndStringSuccessCodes(t *testing.T) {
	responses := []string{
		`{"code":200,"message":"ok","msg_id":"numeric"}`,
		`{"code":"200","message":"ok","account":"3000"}`,
	}
	for _, body := range responses {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Errorf("unexpected request: %s %s", request.Method, request.Header.Get("Content-Type"))
			}
			if err := request.ParseForm(); err != nil || request.Form.Get("app_key") != "secret" {
				t.Errorf("missing form API key: %v", err)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(body))
		}))
		service := testService(t)
		service.WithProviderForTest(server.Client(), server.URL, server.URL)
		if _, err := service.postForm(context.Background(), server.URL, url.Values{"app_key": {"secret"}}); err != nil {
			t.Errorf("success response rejected: %v", err)
		}
		server.Close()
	}
}

func TestProviderRejectsAOKSendErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":40003,"message":"bad template"}`))
	}))
	defer server.Close()
	service := testService(t)
	service.WithProviderForTest(server.Client(), server.URL, server.URL)
	if _, err := service.postForm(context.Background(), server.URL, url.Values{"app_key": {"secret"}}); err == nil {
		t.Fatal("provider error code was accepted")
	}
}
