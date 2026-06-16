package handler

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/storage"
)

// productPageTpl é o template HTML mínimo para a página pública do produto.
// Issue #14: corrige 404 em pay.institutoitinerante.com.br/p/:slug — bloqueio total de GMV.
// OG tags renderizadas para preview no WhatsApp/Instagram. Form usa o endpoint
// /api/v1/public/products/:slug/purchase existente para iniciar checkout PIX.
var productPageTpl = template.Must(template.New("product").Parse(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Name}}</title>
<meta name="description" content="{{.Description}}">
<meta property="og:type" content="product">
<meta property="og:title" content="{{.Name}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.PageURL}}">
{{if .CoverURL}}<meta property="og:image" content="{{.CoverURL}}">{{end}}
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="{{.Name}}">
<meta name="twitter:description" content="{{.Description}}">
{{if .CoverURL}}<meta name="twitter:image" content="{{.CoverURL}}">{{end}}
<style>
*{box-sizing:border-box}
body{margin:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f5f7;color:#111;line-height:1.5}
main{max-width:640px;margin:0 auto;padding:24px 16px}
.card{background:#fff;border-radius:12px;padding:24px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
h1{margin:0 0 12px;font-size:1.6rem}
.desc{white-space:pre-wrap;color:#444;margin:0 0 20px}
.price{font-size:2rem;font-weight:700;color:#0a7;margin:16px 0}
.cover{width:100%;border-radius:8px;margin-bottom:16px;display:block}
label{display:block;margin:10px 0 4px;font-size:.9rem;color:#333}
input{width:100%;padding:10px 12px;border:1px solid #ccc;border-radius:8px;font-size:1rem}
button{width:100%;padding:14px;background:#0a7;color:#fff;border:0;border-radius:8px;font-size:1.05rem;font-weight:600;margin-top:16px;cursor:pointer}
button:disabled{opacity:.6;cursor:not-allowed}
.pix{margin-top:20px;text-align:center}
.pix img{max-width:240px;border:1px solid #eee;border-radius:8px}
.pix textarea{width:100%;margin-top:8px;padding:8px;font-family:monospace;font-size:.8rem;border:1px solid #ccc;border-radius:6px;height:80px}
.note{margin-top:12px;font-size:.85rem;color:#555}
.error{color:#c00;margin-top:8px}
.footer{margin-top:24px;text-align:center;font-size:.8rem;color:#888}
</style>
</head>
<body>
<main>
  <div class="card">
    {{if .CoverURL}}<img class="cover" src="{{.CoverURL}}" alt="{{.Name}}">{{end}}
    <h1>{{.Name}}</h1>
    <p class="desc">{{.Description}}</p>
    <div class="price">{{.PriceFormatted}}</div>

    <form id="buy-form">
      <label for="customer_name">Nome completo</label>
      <input id="customer_name" name="customer_name" required maxlength="255">
      <label for="customer_email">E-mail</label>
      <input id="customer_email" name="customer_email" type="email" required maxlength="255">
      <label for="customer_cpf">CPF (opcional)</label>
      <input id="customer_cpf" name="customer_cpf" maxlength="14">
      <button type="submit" id="buy-btn">Comprar agora</button>
      <div class="error" id="error"></div>
    </form>

    <div class="pix" id="pix" style="display:none">
      <h3>Pague com PIX para liberar o acesso</h3>
      <img id="pix-img" alt="QR Code PIX">
      <p class="note">Escaneie o QR Code ou copie o código abaixo:</p>
      <textarea id="pix-code" readonly></textarea>
      <p class="note">Assim que o pagamento for confirmado, enviaremos o link de acesso por e-mail.</p>
    </div>
  </div>
  <div class="footer">Instituto Itinerante de Tecnologia</div>
</main>
<script>
(function(){
  var form=document.getElementById('buy-form');
  var btn=document.getElementById('buy-btn');
  var err=document.getElementById('error');
  var pix=document.getElementById('pix');
  var pixImg=document.getElementById('pix-img');
  var pixCode=document.getElementById('pix-code');
  form.addEventListener('submit',function(e){
    e.preventDefault();
    err.textContent='';
    btn.disabled=true;btn.textContent='Gerando PIX...';
    var body={
      customer_name:document.getElementById('customer_name').value,
      customer_email:document.getElementById('customer_email').value,
      customer_cpf:document.getElementById('customer_cpf').value,
      payment_method:'pix'
    };
    fetch('/api/v1/public/products/{{.Slug}}/purchase',{
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify(body)
    }).then(function(r){return r.json().then(function(d){return{ok:r.ok,data:d}})})
    .then(function(res){
      if(!res.ok){throw new Error(res.data.error||'erro ao processar compra');}
      var qr=res.data.pix_qr_code||'';
      var copy=res.data.pix_copy_paste||'';
      if(qr){pixImg.src=qr.indexOf('data:')===0?qr:'data:image/png;base64,'+qr;}
      pixCode.value=copy;
      form.style.display='none';
      pix.style.display='block';
    }).catch(function(e){
      err.textContent=e.message;
      btn.disabled=false;btn.textContent='Comprar agora';
    });
  });
})();
</script>
</body>
</html>`))

type productPageData struct {
	Slug           string
	Name           string
	Description    string
	PriceFormatted string
	CoverURL       string
	PageURL        string
}

// RenderPublicProductPage serve a página HTML pública do produto em GET /:slug.
// Issue #14: rota dedicada para compartilhamento em redes sociais (OG tags + checkout PIX).
func (h *LinkPayHandler) RenderPublicProductPage(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return c.Status(fiber.StatusBadRequest).SendString("slug obrigatório")
	}

	product, err := h.service.GetProductBySlug(c.Context(), slug)
	if err != nil || !product.IsActive {
		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.Status(fiber.StatusNotFound).SendString(`<!DOCTYPE html><html lang="pt-BR"><head><meta charset="utf-8"><title>Produto não encontrado</title></head><body style="font-family:sans-serif;text-align:center;padding:40px"><h1>Produto não encontrado</h1><p>Este link pode ter expirado ou estar incorreto.</p></body></html>`)
	}

	cover := ""
	if h.minioClient != nil && h.minioClient.IsConfigured() && product.StorageKey != "" && strings.HasPrefix(product.MimeType, "image/") {
		if url, perr := h.minioClient.PresignedURL(c.Context(), storage.LinkPayBucket, product.StorageKey); perr == nil {
			cover = url
		} else {
			log.Printf("[linkpay] aviso: presigned cover falhou produto=%s: %v", product.ID, perr)
		}
	}

	data := productPageData{
		Slug:           product.Slug,
		Name:           product.Name,
		Description:    product.Description,
		PriceFormatted: formatBRL(product.PriceCents),
		CoverURL:       cover,
		PageURL:        c.Protocol() + "://" + c.Hostname() + c.OriginalURL(),
	}

	var buf bytes.Buffer
	if err := productPageTpl.Execute(&buf, data); err != nil {
		log.Printf("[linkpay] erro ao renderizar página pública: %v", err)
		return c.Status(fiber.StatusInternalServerError).SendString("erro ao renderizar página")
	}

	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Set("Cache-Control", "public, max-age=60")
	return c.Status(fiber.StatusOK).Send(buf.Bytes())
}

// formatBRL formata centavos como "R$ 1.234,56" (separador de milhar "." e decimal ",").
func formatBRL(cents int) string {
	negative := cents < 0
	if negative {
		cents = -cents
	}
	reais := cents / 100
	centavos := cents % 100

	reaisStr := fmt.Sprintf("%d", reais)
	var b strings.Builder
	n := len(reaisStr)
	for i, r := range reaisStr {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	sign := ""
	if negative {
		sign = "-"
	}
	return fmt.Sprintf("R$ %s%s,%02d", sign, b.String(), centavos)
}
