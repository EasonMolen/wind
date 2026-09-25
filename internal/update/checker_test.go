package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckDetectsNewerVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"version":"v1.2.0","downloadUrl":"https://example.test/newwind.zip","notes":"修复搜索"}`))
	}))
	defer server.Close()

	result, err := (Checker{CurrentVersion: "1.1.9", ManifestURL: server.URL}).Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !result.Available || result.LatestVersion != "1.2.0" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckRequiresManifestURL(t *testing.T) {
	_, err := (Checker{CurrentVersion: "1.0.0"}).Check(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Check error = %v, want ErrNotConfigured", err)
	}
}
