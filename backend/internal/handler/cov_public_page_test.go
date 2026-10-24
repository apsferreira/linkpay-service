package handler

// Testes de cobertura (issue #5) para a página pública do produto e formatBRL.

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/storage"
)

func covNewPageApp(t *testing.T, svc *covLinkPayStub, minio *storage.MinIOClient) *fiber.App {
	t.Helper()
	h := covNewLinkPay(t, svc, minio)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/p/:slug", h.RenderPublicProductPage)
	return app
}

func TestCovFormatBRL(t *testing.T) {
	cases := []struct {
		cents int
		want  string
	}{
		{0, "R$ 0,00"},
		{5, "R$ 0,05"},
		{99, "R$ 0,99"},
		{100, "R$ 1,00"},
		{2990, "R$ 29,90"},
		{100000, "R$ 1.000,00"},
		{123456, "R$ 1.234,56"},
		{100000000, "R$ 1.000.000,00"},
		{-2990, "R$ -29,90"},
		{-5, "R$ -0,05"},
	}

	for _, tc := range cases {
		if got := formatBRL(tc.cents); got != tc.want {
			t.Errorf("formatBRL(%d) = %q, queria %q", tc.cents, got, tc.want)
		}
	}
}

func TestCovRenderPublicProductPage(t *testing.T) {
	t.Run("slug vazio", func(t *testing.T) {
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app.Get("/p", h.RenderPublicProductPage)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("produto não encontrado", func(t *testing.T) {
		app := covNewPageApp(t, &covLinkPayStub{getBySlugErr: errors.New("produto não encontrado")}, nil)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p/inexistente", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type = %q", ct)
		}
		raw, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(raw), "Produto não encontrado") {
			t.Error("HTML de 404 não contém a mensagem esperada")
		}
	})

	t.Run("produto inativo", func(t *testing.T) {
		app := covNewPageApp(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{ID: "p1", Slug: "curso", IsActive: false},
		}, nil)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p/curso", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("sucesso sem cover", func(t *testing.T) {
		app := covNewPageApp(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{
				ID: "p1", Slug: "curso", Name: "Curso de Go", Description: "Descrição",
				PriceCents: 123456, IsActive: true,
			},
		}, nil)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p/curso", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		if cache := resp.Header.Get("Cache-Control"); cache != "public, max-age=60" {
			t.Errorf("Cache-Control = %q", cache)
		}
		raw, _ := io.ReadAll(resp.Body)
		html := string(raw)
		for _, want := range []string{"Curso de Go", "R$ 1.234,56", "og:type"} {
			if !strings.Contains(html, want) {
				t.Errorf("HTML não contém %q", want)
			}
		}
	})

	t.Run("sucesso com cover via minio", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		app := covNewPageApp(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{
				ID: "p1", Slug: "curso", Name: "Curso", PriceCents: 500,
				StorageKey: "tenant/products/p1/cover.png", MimeType: "image/png", IsActive: true,
			},
		}, minio)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p/curso", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		raw, _ := io.ReadAll(resp.Body)
		html := string(raw)
		if !strings.Contains(html, "og:image") {
			t.Error("HTML deveria conter og:image quando há cover")
		}
		if !strings.Contains(html, "cover.png") {
			t.Error("HTML deveria conter a URL do cover")
		}
	})

	t.Run("cover ignorado quando mime não é imagem", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		app := covNewPageApp(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{
				ID: "p1", Slug: "curso", Name: "Curso", PriceCents: 500,
				StorageKey: "tenant/products/p1/a.pdf", MimeType: "application/pdf", IsActive: true,
			},
		}, minio)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p/curso", nil))
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(raw), "og:image") {
			t.Error("HTML não deveria conter og:image para mime não-imagem")
		}
	})
}
