# Mak1zu (resumen en español)

Motor en Go para una compañera de Discord con memoria, personalidad propia, panel web y SDK. Linux primero, cualquier distro, un solo binario estático (sin CGO).

Nace de una compañera privada que armé y corrí durante meses en una comunidad de Discord, hablando con gente real todos los días. Es la primera vez que algo de esto sale al público. Todo lo que la hacía sentir una persona (respuestas cortas, opiniones, referencias a charlas pasadas, malos días, no sonar a soporte técnico) está codificado como reglas medidas y código con tests. Cada quien le pone la personalidad que quiera encima de esa base. Ver [docs/VOICE.md](docs/VOICE.md) (en inglés).

```bash
mak1zu init ~/mak1zu && cd ~/mak1zu
# poné MAK1ZU_API_KEY (y MAK1ZU_DISCORD_TOKEN) en .env
mak1zu doctor && mak1zu chat     # probala en la terminal
mak1zu run                       # Discord + panel en http://127.0.0.1:8787
```

- Todo lo que la define vive en la carpeta `.makizu/` (personalidades, reglas, skills, notas por servidor y por canal): Markdown plano, editable a mano o desde el panel, se relee en cada mensaje. Plantilla de personalidad en `docs/PERSONA_TEMPLATE.md`.
- Hoja de ruta: [ROADMAP.md](ROADMAP.md).
