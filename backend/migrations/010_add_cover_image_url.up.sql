-- #16: cover_image_url para produtos digitais (WhatsApp og:image)
ALTER TABLE digital_products
  ADD COLUMN cover_image_url TEXT;
