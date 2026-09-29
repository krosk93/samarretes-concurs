# Mock local d'Appsistència

Mock amb només stdlib que imita el flux que usa `appsistencia.go`:
`GET /` (prime `PHPSESSID`) + `POST /` amb formulari de login (`302` = èxit)
i `GET /api/castellers?order=asc` amb cookie.

## Arrencar

```sh
# Directe (defaults: port 8081, usuari test/test)
cd mock && go run .
# o
PORT=8081 MOCK_USER=test MOCK_PASSWORD=test go run .

# Amb Docker Compose (aixeca samarretes + mock)
docker compose up --build
# Mock a http://localhost:8081 (port amb ${MOCK_PORT:-8081})
```

Env suportat: `PORT` (default `8081`), `MOCK_USER` (default `test`),
`MOCK_PASSWORD` (default `test`).

## Credencials default

- Usuari: `test`
- Password: `test`

## Exemples curl

```sh
BASE=http://localhost:8081
JAR=$(mktemp)

# 1. Prime sessió anònima
curl -c "$JAR" "$BASE/" -o /dev/null

# 2a. Login KO -> 200 amb form login
curl -b "$JAR" -c "$JAR" -d "username=test&password=malament&submitted=1&email_confirm=&phone=" "$BASE/" -o /dev/null -w "%{http_code}\n"

# 2b. Login OK -> 302 Location /go/panel
curl -b "$JAR" -c "$JAR" --max-redirs 0 -d "username=test&password=test&submitted=1&email_confirm=&phone=" "$BASE/" -D - -o /dev/null

# 3. GET sense sessió -> 401
curl http://localhost:8081/api/castellers?order=asc

# 4. GET amb sessió -> 200 {"total":N,"rows":[...]}
curl -b "$JAR" "$BASE/api/castellers?order=asc"

# 5. POST alta (com el curl d'usuari: JSON cru amb Content-Type form)
curl -b "$JAR" "$BASE/api/castellers" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  --data-binary '{"nom":"ProvaNova","cognom1":"CognomNou","dni":"99999999Z","telefon":"600999888"}'

# Utilitats
curl "$BASE/health"          # {"ok":true}
curl "$BASE/__mock/info"     # user, sessions, num castellers, last POST
curl -X POST "$BASE/__mock/reset"  # recarrega castellers.json
```

## Nota POST

`POST /api/castellers` llegeix el body sencer (fins 2MB) i primer prova
`json.Unmarshal` directe. Si falla, fa `ParseForm` i prova cada valor/clau
com JSON. Per això accepta JSON cru encara que el `Content-Type` sigui
`application/x-www-form-urlencoded` (com el curl del formulari real).
Respon `201` amb l'objecte creat (amb `id` nou = max+1), `401` sense sessió
vàlida i `400` si el body no és JSON.
