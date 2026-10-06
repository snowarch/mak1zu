# Mak1zu: brand

La hoja con todo junto es `hoja-marca.png`.

## La idea

La chica del dibujo original (`mak1zu2`) está delante de una luna. La luna no es un disco con
relleno: es una trama de manga. Su borde, la sombra y el busto, que se disuelve en la noche, están
hechos de los mismos puntos. Por eso ningún borde vectorial corta una trama.

## Modos

| Modo | Qué es | Fondo |
| --- | --- | --- |
| Luna | Ella en tinta, con una luna de puntos de papel detrás. | Oscuro |
| Eclipse | El negativo: ella en blanco, con una luna de puntos bermellón detrás. | Oscuro |
| Gris | El negativo sin color. Solo wallpaper, el más calmo. | Oscuro |
| Sol | Un disco bermellón de puntos sobre papel, con ella en tinta. | Claro |

## Piezas (en `svg/` y `png/`; los íconos chicos, en `icon/`)

- **Emblemas:** `emblema-luna`, `emblema-eclipse` y `emblema-sol`.
- **Wordmark:** `wordmark-claro` (para fondo oscuro), `wordmark-oscuro` (para fondo claro) y
  `wordmark-una-tinta`.
- **Avatares:** `avatar-luna`, `avatar-eclipse` y `avatar-sol`. Se pueden recortar en círculo. El
  de Sol lleva un aro de papel, porque es la versión clara.
- **Íconos:** `icono-*`, de 16 a 256 px, sin trama.
- **Banners:** `banner-*` de 1500×500 (README, X, YouTube) y `social-*` de 1280×640 (vista previa
  de GitHub).
- **Wallpapers:** `wallpaper-{luna,eclipse,gris,sol}-{1080p,4k}`, sin texto.

Todo está en trazos y no depende de fuentes instaladas.

## Color

- Sumi (tinta) `#121114`
- Washi (papel) `#F3F0EA`
- Shu (bermellón) `#E4412B`

## Reglas

- Cada trama es de una sola tinta y de puntos enteros. El paso va en píxeles de salida (unos
  3 px) y el ángulo es de 45°. En Sol, el rojo va a 15°, para que las dos tramas no hagan moiré.
- Nunca un borde sólido junto a una trama. Donde la trama se vuelve sólida, el sólido sale del
  mismo campo de tono, así el borde queda justo donde los puntos ya se unieron.
- Por debajo de 64 px no hay trama. Ahí van los `icono-*`.
- Los wallpapers no llevan wordmark, porque en el escritorio choca con los widgets.
- No recortar a Maki con placas ni contornos.

## Regenerar

`fuente/final2.py` reescribe `svg/`. Necesita Python con `numpy`, `fontTools` y `potracer`. Los
PNG salen con `rsvg-convert`.

## License of the brand

The code in this repository is Apache-2.0 (see ../LICENSE and ../NOTICE). The
name "Mak1zu", the logo, the wordmark and all artwork in this folder are
copyright snowarch, all rights reserved: you may use them to refer to the
original project (links, articles, screenshots), but a fork or derivative bot
must use its own name and artwork. The one exception is the generator in
`source/`, which is Apache-2.0 like the rest of the code.

## Regenerar sin "Studio"

`source/final2.py` escribe `svg/` (ya no dibuja la palabra STUDIO) y
`source/render.sh` saca los PNG. Dependencias de Python: numpy, fonttools,
potracer (en un venv).
