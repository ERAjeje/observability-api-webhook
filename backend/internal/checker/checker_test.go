package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"monitor/internal/domain"
)

func epOK(srv *httptest.Server) domain.Endpoint {
	return domain.Endpoint{
		ID: 1, Name: "api", URL: srv.URL, Method: http.MethodGet,
		Interval: time.Minute, Timeout: 2 * time.Second,
	}
}

func TestRun_HTTP200_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out := New(true).Run(context.Background(), epOK(srv))
	if out.Result != domain.ResultOK {
		t.Fatalf("esperado ok; got %s (%s)", out.Result, out.ErrorDetail)
	}
	if out.HTTPStatus != 200 {
		t.Fatalf("status esperado 200; got %d", out.HTTPStatus)
	}
}

func TestRun_HTTP500_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	out := New(true).Run(context.Background(), epOK(srv))
	if out.Result != domain.ResultFail || out.HTTPStatus != 500 {
		t.Fatalf("esperado fail/500; got %s/%d", out.Result, out.HTTPStatus)
	}
}

func TestRun_Timeout_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	ep := epOK(srv)
	ep.Timeout = 50 * time.Millisecond
	start := time.Now()
	out := New(true).Run(context.Background(), ep)
	if out.Result != domain.ResultFail {
		t.Fatalf("esperado fail por timeout; got %s", out.Result)
	}
	if !strings.Contains(out.ErrorDetail, "timeout") {
		t.Fatalf("esperado detalhe de timeout; got %q", out.ErrorDetail)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout deve cortar a checagem cedo")
	}
}

func TestRun_ExpectStatus_Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated) // 201
	}))
	defer srv.Close()

	ep := epOK(srv)
	ep.ExpectStatus = 200
	out := New(true).Run(context.Background(), ep)
	if out.Result != domain.ResultFail {
		t.Fatalf("esperado fail (201 != esperado 200); got %s", out.Result)
	}
}

func TestRun_ExpectBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok: healthy v3"))
	}))
	defer srv.Close()

	ok := epOK(srv)
	ok.ExpectBody = "healthy"
	if out := New(true).Run(context.Background(), ok); out.Result != domain.ResultOK {
		t.Fatalf("esperado ok (body contém); got %s", out.Result)
	}

	bad := epOK(srv)
	bad.ExpectBody = "mismatch"
	if out := New(true).Run(context.Background(), bad); out.Result != domain.ResultFail {
		t.Fatalf("esperado fail (body não contém); got %s", out.Result)
	}
}

func TestRun_DegradedPorLatencia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := epOK(srv)
	ep.LatencyThreshold = 50 * time.Millisecond
	out := New(true).Run(context.Background(), ep)
	if out.Result != domain.ResultDegraded {
		t.Fatalf("esperado degraded (latencia > limiar); got %s", out.Result)
	}
}

func TestRun_HeaderEnviado(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ep := epOK(srv)
	ep.Headers = map[string]string{"Authorization": "Bearer abc"}
	New(true).Run(context.Background(), ep)
	if gotAuth != "Bearer abc" {
		t.Fatalf("header não enviado; got %q", gotAuth)
	}
}
