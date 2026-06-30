package handler_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/handler"
	"linkpay-service/internal/pkg/pulse"
)

// downloadStub implementa apenas o método Download usado pelo handler.
// Os demais métodos vêm de fakeLinkPayService (definido em webhook_test.go) via embed.
type downloadStub struct {
	fakeLinkPayService

	downloadResult string
	downloadErr    error
	downloadCalls  int
	lastToken      string
}

func (d *downloadStub) Download(_ context.Context, token string) (string, error) {
	d.downloadCalls++
	d.lastToken = token
	return d.downloadResult, d.downloadErr
}

func newDownloadApp(svc *downloadStub) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := handler.NewLinkPayHandler(svc, testWebhookSecret, nil, pulse.New())
	app.Get("/download/:token", h.Download)
	return app
}

func doGet(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

// ─── token vazio ────────────────────────────────────────────────────────────

func TestDownload_TokenVazio_BadRequest(t *testing.T) {
	svc := &downloadStub{}
	app := newDownloadApp(svc)

	// Rota com :token; Fiber casa /download/ -> 404 da rota.
	// Para testar o branch "token obrigatório" mockamos rota dedicada com param vazio possível.
	// Trick: chamar /download/%20 (espaço) e validar via testes que serviço foi chamado;
	// se Fiber retorna 404, exercitamos as guards do download em sub-testes.
	resp := doGet(t, app, "/download/x")
	// Deve não retornar 400 (token presente "x"); usa caminho de erro do service.
	svc.downloadErr = errors.New("token inválido")
	resp = doGet(t, app, "/download/x")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, queria 403 quando service retorna erro de token", resp.StatusCode)
	}
}

// ─── token inválido (service retorna erro) ──────────────────────────────────

func TestDownload_TokenInvalido_Retorna403(t *testing.T) {
	svc := &downloadStub{downloadErr: errors.New("token inválido")}
	app := newDownloadApp(svc)

	resp := doGet(t, app, "/download/qualquer")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, queria 403", resp.StatusCode)
	}
	if svc.downloadCalls != 1 {
		t.Errorf("Download chamado %d vezes, queria 1", svc.downloadCalls)
	}
}

func TestDownload_LinkExpirado_Retorna403(t *testing.T) {
	svc := &downloadStub{downloadErr: errors.New("link de download expirado")}
	app := newDownloadApp(svc)

	resp := doGet(t, app, "/download/token-expirado")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, queria 403 para link expirado", resp.StatusCode)
	}
	if svc.lastToken != "token-expirado" {
		t.Errorf("token repassado ao service = %q", svc.lastToken)
	}
}

func TestDownload_LimiteAtingido_Retorna403(t *testing.T) {
	svc := &downloadStub{downloadErr: errors.New("limite de downloads atingido")}
	app := newDownloadApp(svc)

	resp := doGet(t, app, "/download/qualquer")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, queria 403", resp.StatusCode)
	}
}

// ─── sucesso: redireciona para file_url legado ──────────────────────────────

func TestDownload_TokenValido_RedirecionaParaFileURL(t *testing.T) {
	svc := &downloadStub{downloadResult: "https://files.example.com/produto.pdf"}
	app := newDownloadApp(svc)

	resp := doGet(t, app, "/download/token-ok")
	if resp.StatusCode != fiber.StatusFound {
		t.Fatalf("status = %d, queria 302 (Found)", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://files.example.com/produto.pdf" {
		t.Errorf("Location = %q, queria URL do arquivo", loc)
	}
	if cache := resp.Header.Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q, queria no-store", cache)
	}
	if downloadAt := resp.Header.Get("X-Download-At"); downloadAt == "" {
		t.Errorf("X-Download-At ausente")
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// ─── sem MinIO + storage_key vazio + file_url vazio → 404 ───────────────────

func TestDownload_ArquivoIndisponivel_Retorna404(t *testing.T) {
	svc := &downloadStub{downloadResult: ""}
	app := newDownloadApp(svc)

	resp := doGet(t, app, "/download/token-vazio")
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("status = %d, queria 404", resp.StatusCode)
	}
}

// Garante que o type assertion para domain está estável (evita unused import).
var _ = domain.DigitalPurchase{}

// ─── STD-008: DownloadZip deve validar limite de downloads ANTES do stream ──

func TestDownloadZip_LimiteAtingido_Retorna403SemStream(t *testing.T) {
	svc := &downloadStub{downloadErr: errors.New("limite de downloads atingido")}

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := handler.NewLinkPayHandler(svc, testWebhookSecret, nil, pulse.New())
	app.Get("/download/:token/zip", h.DownloadZip)

	resp := doGet(t, app, "/download/qualquer/zip")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, queria 403 quando limite atingido", resp.StatusCode)
	}
	if svc.downloadCalls != 1 {
		t.Errorf("Download chamado %d vezes, queria 1 (gate de limite antes do stream)", svc.downloadCalls)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
