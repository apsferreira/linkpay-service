package handler

// LGPD (Lei 13.709/2018) — endpoints de titularidade do comprador (Art. 18):
//   - DELETE /api/v1/public/data-erasure   (inciso VI — eliminação)
//   - POST  /api/v1/public/data-portability (inciso V — portabilidade)
//
// O linkpay-service não possui conta de usuário: a identidade do titular é
// provada por email + access_token de uma compra/assinatura (service valida).

import (
	"context"
	"log"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
)

// DataSubjectServiceInterface expõe as operações LGPD Art. 18 usadas pelo handler.
type DataSubjectServiceInterface interface {
	EraseBuyerData(ctx context.Context, email, token string) (*domain.BuyerDataErasureResult, error)
	ExportBuyerData(ctx context.Context, email, token string) (*domain.BuyerDataExport, error)
}

// DataSubjectHandler atende os endpoints de direitos do titular.
type DataSubjectHandler struct {
	service   DataSubjectServiceInterface
	validator *validator.Validate
}

func NewDataSubjectHandler(service DataSubjectServiceInterface) *DataSubjectHandler {
	return &DataSubjectHandler{
		service:   service,
		validator: validator.New(),
	}
}

// parseBuyerDataRequest faz o parse, valida e normaliza o corpo comum dos dois
// endpoints de titularidade. Em erro, já escreve a resposta 400 e retorna false.
func (h *DataSubjectHandler) parseBuyerDataRequest(c *fiber.Ctx) (domain.BuyerDataRequest, bool) {
	var req domain.BuyerDataRequest
	if err := c.BodyParser(&req); err != nil {
		_ = c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
		return req, false
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Token = strings.TrimSpace(req.Token)
	if err := h.validator.Struct(&req); err != nil {
		_ = c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email válido e token são obrigatórios"})
		return req, false
	}
	return req, true
}

// DataErasure godoc
//
//	@Summary      Eliminação de dados do titular (LGPD Art. 18, VI)
//	@Description  Anonimiza os dados pessoais (nome, email, CPF) das compras e assinaturas vinculadas ao email informado. A titularidade é comprovada por email + access_token de uma compra/assinatura do titular. Os registros financeiros são mantidos (retenção fiscal — Art. 16).
//	@Tags         lgpd
//	@Accept       json
//	@Produce      json
//	@Param        body  body      domain.BuyerDataRequest  true  "Email e token do titular"
//	@Success      200   {object}  map[string]interface{}
//	@Failure      400   {object}  map[string]string
//	@Failure      403   {object}  map[string]string
//	@Router       /api/v1/public/data-erasure [delete]
func (h *DataSubjectHandler) DataErasure(c *fiber.Ctx) error {
	req, ok := h.parseBuyerDataRequest(c)
	if !ok {
		return nil
	}

	res, err := h.service.EraseBuyerData(c.Context(), req.Email, req.Token)
	if err != nil {
		log.Printf("[lgpd] data erasure rejeitado: %v", err)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "titularidade não confirmada — verifique email e token"})
	}

	return c.JSON(fiber.Map{
		"message":              "dados pessoais anonimizados",
		"purchases_erased":     res.PurchasesErased,
		"subscriptions_erased": res.SubscriptionsErased,
	})
}

// DataPortability godoc
//
//	@Summary      Portabilidade de dados do titular (LGPD Art. 18, V)
//	@Description  Retorna em JSON estruturado os dados pessoais tratados pelo linkpay-service para o email informado (compras e assinaturas). A titularidade é comprovada por email + access_token de uma compra/assinatura do titular.
//	@Tags         lgpd
//	@Accept       json
//	@Produce      json
//	@Param        body  body      domain.BuyerDataRequest  true  "Email e token do titular"
//	@Success      200   {object}  domain.BuyerDataExport
//	@Failure      400   {object}  map[string]string
//	@Failure      403   {object}  map[string]string
//	@Router       /api/v1/public/data-portability [post]
func (h *DataSubjectHandler) DataPortability(c *fiber.Ctx) error {
	req, ok := h.parseBuyerDataRequest(c)
	if !ok {
		return nil
	}

	export, err := h.service.ExportBuyerData(c.Context(), req.Email, req.Token)
	if err != nil {
		log.Printf("[lgpd] data portability rejeitado: %v", err)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "titularidade não confirmada — verifique email e token"})
	}

	return c.JSON(export)
}
