---
created_at: 2026-09-04T07:23:54.743705338Z
updated_at: 2026-09-04T07:23:54.743705338Z
---
# Optimización de imágenes para web

Se generaron versiones JPEG optimizadas para web de las captures de la carpeta `assets`:

- `assets/tui.png` -> `assets/tui.jpg`
- `assets/webui-desktop.png` -> `assets/webui-desktop.jpg`

## Motivo
Las versiones JPG reducen drásticamente el tamaño de archivo sin un coste visual significativo para pantallas de captura, y son más adecuadas para la web o interfaces con carga de imágenes.

## Resultado
- `tui.png`: 434,538 bytes
- `tui.jpg`: 154,122 bytes (~35.5% del original)
- `webui-desktop.png`: 343,088 bytes
- `webui-desktop.jpg`: 148,014 bytes (~43.1% del original)

## Verificación
Se generaron con Python 3 + Pillow, usando `quality=72`, `optimize=True` y `progressive=True` para mantener una buena calidad visual y una menor carga.
