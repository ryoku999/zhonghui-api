# Ejecutar y desplegar la API de Zhonghui

La imagen compila Go 1.27.4 en una etapa y ejecuta el binario como usuario sin privilegios en otra. El contenedor escucha en el puerto `8000`. Docker Compose publica ese puerto en `127.0.0.1`. PostgreSQL se administra por separado.

## Docker local con la base del proyecto

1. Levanta PostgreSQL desde `zhonghui-db/` con `docker compose up -d`. El Compose de la API se conecta a la red `zhonghui-db_default`. Si el proyecto de Compose de PostgreSQL tiene otro nombre, configura `ZHONGHUI_DB_NETWORK` con el nombre que aparezca en `docker network ls`.
2. Desde `zhonghui-api/`, copia tu entorno de Go para usar una configuración independiente.

```powershell
Copy-Item .env .env.docker
$env:ZHONGHUI_API_ENV_FILE = '.env.docker'
```

En `.env.docker`, conserva el usuario, la contraseña y la base local y cambia el host de `DATABASE_URL` a `postgres`, que es el nombre del servicio en el Compose de la base. Por ejemplo:

```dotenv
DATABASE_URL=postgresql://USUARIO:CONTRASENA@postgres:5432/zhonghui
```

3. Levanta la API y verifica su estado.

```powershell
docker compose -f compose.yml -f compose.local-db.yml up --build -d --wait
docker compose ps
Invoke-RestMethod http://127.0.0.1:8000/api/v1/health/ready
```

Para probar Docker local contra Neon después de aplicar las migraciones, usa la URL pooled de Neon como `DATABASE_URL` de `.env.docker` y ejecuta `docker compose up --build -d --wait` sin el archivo `compose.local-db.yml`.

El proxy de Vite local apunta a `8080`. Si deseas probar la web local con el contenedor en `8000`, cambia el destino de ese proxy a `http://127.0.0.1:8000` y reinicia Vite.

## Preparar el esquema en Neon

Rota la contraseña de Neon si la URL anterior fue compartida. En **Connect**, obtén la URL directa con **Connection pooling** desactivado. Usa la URL exacta que emite Neon, incluidos sus parámetros de TLS. La API puede usar la URL pooled de ese mismo proyecto, rama y base. [Conexión directa y pooled en Neon](https://neon.com/docs/connect/connection-pooling)

Desde `zhonghui-api/`, aplica el esquema con Goose. La entrada de la URL no se muestra en PowerShell.

```powershell
$env:GOOSE_DRIVER = 'postgres'
$env:GOOSE_DBSTRING = Read-Host 'URL directa de Neon' -MaskInput

goose -env none -dir db/migrations status
goose -env none -dir db/migrations up
goose -env none -dir db/migrations status

Remove-Item Env:GOOSE_DBSTRING
```

El último estado debe mostrar las migraciones aplicadas hasta la versión `5`. Si cualquier comando devuelve un error, revisa el error antes de continuar. Goose crea el esquema. La base nueva requiere además un ADMIN y los datos iniciales que quieras usar.

### Trasladar los datos locales

Para conservar el ADMIN y los registros que ya funcionan en local, restaura un respaldo de solo datos sobre una base Neon recién migrada, antes de cargar registros allí. El ejemplo usa las herramientas de PostgreSQL del contenedor local. Excluye el historial de Goose y las sesiones, por lo que tendrás que iniciar sesión de nuevo. El archivo de respaldo puede contener datos personales y hashes de contraseña. [Guía de migración de Neon](https://neon.com/docs/import/migrate-from-postgres)

```powershell
docker exec zhonghui-postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --data-only --exclude-table-data=public.goose_db_version --exclude-table-data=public.user_sessions --file=/tmp/zhonghui-data.dump'
docker cp zhonghui-postgres:/tmp/zhonghui-data.dump .\zhonghui-data.dump

$env:ZHONGHUI_NEON_DIRECT_URL = Read-Host 'URL directa de Neon' -MaskInput
docker exec -e ZHONGHUI_NEON_DIRECT_URL zhonghui-postgres sh -c 'pg_restore --data-only --no-owner --no-privileges --single-transaction --dbname="$ZHONGHUI_NEON_DIRECT_URL" /tmp/zhonghui-data.dump'
Remove-Item Env:ZHONGHUI_NEON_DIRECT_URL
```

Revisa la salida de cada comando antes de pasar al siguiente. Puedes importar los datos iniciales por DBeaver si quieres elegir qué trasladar, incluido un ADMIN existente con su hash Argon2id.

## Render

Crea un **Web Service** desde el repositorio de la API con **Docker** como lenguaje. Si el repositorio ya tiene `Dockerfile` en la raíz, usa `./Dockerfile` y su directorio como contexto. Si está dentro de un repositorio mayor, establece `zhonghui-api` como directorio raíz del servicio. Render construye y arranca la imagen con el Dockerfile. [Docker en Render](https://render.com/docs/docker)

| Variable en Render | Valor |
| --- | --- |
| `PORT` | `8000` |
| `APP_ENV` | `production` |
| `DATABASE_URL` | URL pooled nueva de Neon |
| `COOKIE_SECURE` | `true` |
| `CORS_ALLOWED_ORIGINS` | `https://registro-transportistas.vercel.app` |
| `JWT_ISSUER`, `JWT_AUDIENCE` | `zhonghui-api` y `zhonghui-web` |
| `JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET` | Dos claves distintas, cada una de 32 bytes aleatorios codificados en Base64 |
| `APISTED_BASE_URL`, `APISTED_TOKEN` | URL y token del proveedor si habilitas las consultas |

Deja `HTTP_ADDR` sin definir para que Go use `0.0.0.0:$PORT`. Configura **Health Check Path** con `/api/v1/health/ready`. Render expone un dominio HTTPS, y el puerto `8000` queda dentro del contenedor. [Puertos de Render Web Services](https://render.com/docs/web-services)

Primero valida `GET /api/v1/health/ready` en el dominio de Render. Luego configura el proxy de Vercel cuando retomes el despliegue de la web.
