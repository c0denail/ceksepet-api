# ÇekSepet Backend API

ÇekSepet mobil uygulamasının Go ile geliştirilmiş REST API servisidir. Kayıt/giriş ve kullanıcı yönetiminin yanında ürün ekleme, fotoğraf yükleme, çekiliş listeleri, favoriler, bilet cüzdanı, onaylı katılım, soru-cevap, satıcı değerlendirmeleri ve bildirimleri sağlar. Veriler MongoDB'de kalıcı tutulur; HTTP akışı Swagger UI üzerinden denenebilir.

Altı mobil ekranın alan eşlemesi, yeni endpointler, fiyat/komisyon kuralları ve baştan sona deneme adımları: **[Ürün ve çekiliş API rehberi](docs/marketplace-api.md)**. Yeni ürünler operatör onayından sonra yayına girer. Varsayılan hesap 1 bilet = 1 TL, komisyon %10'dur; `.env` içindeki `TICKET_VALUE_KURUS` ve `COMMISSION_BPS` ile değiştirilir. Yeni hesapların bilet bakiyesi sıfırdır.

## Temel özellikler

- İki adımlı kullanıcı kaydı ve kayıt SMS doğrulaması
- Telefon + 6 haneli PIN + giriş SMS doğrulaması
- Mobil uygulamaya uygun geçici kayıt ve giriş tokenları
- MongoDB Atlas üzerinde kalıcı kullanıcı ve sunucu taraflı oturum yönetimi
- Bearer access token ile kullanıcı işlemleri
- Refresh token kullanmadan logout ile sunucu taraflı token iptali
- Kullanıcı adı ve PIN doğrulamasıyla hesap silme
- Ayrı yetkilere sahip süreli test operatörü tokenı
- Tokensız, kişisel verileri filtrelenmiş public kullanıcı listesi
- Swagger UI ve OpenAPI 3 tanımı
- Rate limit, OTP deneme sınırı, TTL indexleri ve güvenli hashleme
- Docker ve Docker Compose desteği

## Kullanılan teknolojiler

- Go 1.24+
- Standart `net/http` yönlendirmesi
- MongoDB Atlas ve resmi MongoDB Go Driver
- bcrypt tabanlı PIN hashleme
- HMAC tabanlı OTP ve opaque token özeti
- OpenAPI 3 + Swagger UI

## Gereksinimler

- Go 1.24 veya üzeri
- Erişilebilir bir MongoDB Atlas cluster'ı
- Atlas **Network Access** bölümünde uygulamayı çalıştıran IP adresine izin verilmesi
- İsteğe bağlı olarak Docker ve Docker Compose

## Kurulum

Repository'yi klonlayın ve proje klasörüne geçin:

```bash
git clone https://github.com/KULLANICI_ADIN/CekSepet.git
cd CekSepet
go mod download
```

Yapılandırma dosyasını oluşturun:

```bash
cp .env.example .env
```

En az `MONGODB_URI` değerini kendi Atlas bağlantınızla doğrulayın. Uygulama proje klasöründeki veya üst klasörlerden birindeki `.env` dosyasını başlangıçta otomatik yükler.

API'yi başlatın:

```bash
make run
```

Doğrudan Go komutuyla da çalıştırabilirsiniz:

```bash
go run .
```

Bu komut proje kökündeki `main.go` girişini çalıştırır. Önceki `go run ./cmd/api` komutu da aynı başlangıç kodunu kullanır ve çalışmaya devam eder.

Başarılı başlangıçtan sonra:

- Swagger UI: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)
- OpenAPI YAML: [http://localhost:8080/swagger/openapi.yaml](http://localhost:8080/swagger/openapi.yaml)
- Liveness: [http://localhost:8080/health/live](http://localhost:8080/health/live)
- Readiness: [http://localhost:8080/health/ready](http://localhost:8080/health/ready)

Uygulama açılırken MongoDB bağlantısını kontrol eder ve gerekli unique/TTL indexlerini idempotent olarak oluşturur. Atlas bağlantısı kurulamazsa HTTP sunucusu başlatılmaz.

## Swagger ile hızlı kayıt testi

Development ortamında gönderilen OTP kodu cevap içindeki `debugOtpCode` alanında gösterilir. Böylece gerçek bir SMS sağlayıcısı olmadan bütün akış Swagger üzerinden denenebilir.

1. `POST /api/v1/auth/register/step-1`
   - `firstName`, `lastName`, `phone` ve tam 6 rakamlık `pin` gönderin.
   - Dönen `registrationToken` değerini sonraki kayıt çağrılarında kullanın.
2. `GET /api/v1/avatars`
   - Kullanılabilir avatarları alın ve bir `avatarId` seçin.
3. `POST /api/v1/auth/register/step-2`
   - `registrationToken` ve `avatarId` gönderin.
   - Sistem benzersiz kullanıcı adını üretir ve kayıt OTP'sini gönderir.
4. `POST /api/v1/auth/register/otp/verify`
   - `registrationToken` ve `debugOtpCode` değerini `otpCode` olarak gönderin.
   - Kullanıcı MongoDB'ye kaydedilir; kullanıcı bilgileri ile `accessToken` döner.
5. Swagger üst bölümündeki **Authorize** düğmesinden `bearerAuth` alanına access tokenı girin.
6. `GET /api/v1/users/me` ile oturumdaki kullanıcıyı doğrulayın.

## Kullanıcı kayıt akışı

Kayıt ilk istekte kalıcı kullanıcı oluşturmaz. Birinci adımda kişisel bilgiler geçici `registration_sessions` kaydına yazılır. İkinci adımda avatar seçilir, kullanıcı adı üretilir ve OTP gönderilir. Kullanıcı ancak kayıt OTP'si başarıyla doğrulandığında `users` koleksiyonuna kaydedilir.

`registrationToken` varsayılan olarak 30 dakika geçerlidir ve mobil uygulamanın kalıcı depolamasına yazılmamalıdır. OTP tekrar gönderimi için `POST /api/v1/auth/register/otp/resend` kullanılabilir.

## Giriş ve access token akışı

1. `POST /api/v1/auth/login` ile telefon ve 6 haneli PIN doğrulanır.
2. Başarılı kontrolden sonra access token yerine geçici `loginToken` ve SMS OTP gönderilir.
3. `POST /api/v1/auth/login/otp/verify` ile `loginToken` ve OTP doğrulanır.
4. Doğrulama başarılıysa kullanıcı bilgileri ve access token döner.

Access token opaque bir değerdir; MongoDB'de tokenın kendisi değil yalnızca HMAC özeti saklanır. Mobil uygulama tokenı iOS Keychain veya Android Keystore gibi güvenli depolamada tutmalıdır. Uygulama yeniden açıldığında saklanan token geçerliyse kullanıcıdan tekrar giriş istenmez.

Bu projede refresh token yoktur. Müşteri access tokenının süre sınırı bulunmaz; logout veya hesap silme işlemine kadar geçerlidir. `POST /api/v1/auth/logout` ilgili oturumu sunucu tarafında iptal eder. Çalınmış token riskini azaltmak için production aşamasında cihaz oturumlarını listeleme/sonlandırma veya süreli access token stratejisi değerlendirilmelidir.

## Test operatörü

Operatör özelliği `.env` içindeki `OPERATOR_USERNAME` ve `OPERATOR_PASSWORD` birlikte tanımlandığında etkinleşir.

1. `POST /api/v1/operator/login` ile operatör tokenı alın.
2. Swagger'daki **Authorize → operatorBearerAuth** alanına tokenı girin.
3. `GET /api/v1/operator/users` ile telefon dahil müşteri listesini alın.
4. `DELETE /api/v1/operator/users/{userId}` ile seçilen müşteriyi silin.
5. `POST /api/v1/operator/logout` ile operatör oturumunu kapatın.

Operatör tokenı varsayılan olarak sekiz saat geçerlidir ve müşteri tokenı yerine kullanılamaz. MongoDB'de yalnızca token özeti saklanır.

## Endpoint özeti

| Yöntem | Endpoint | Yetki | Açıklama |
|---|---|---|---|
| GET | `/health/live` | Yok | Uygulama yaşam kontrolü |
| GET | `/health/ready` | Yok | MongoDB dahil hazır olma kontrolü |
| GET | `/api/v1/avatars` | Yok | Avatar kataloğunu listeler |
| GET | `/avatars/{filename}` | Yok | Avatar dosyasını döndürür |
| POST | `/api/v1/auth/register/step-1` | Yok | Kayıt bilgileri, telefon ve PIN |
| POST | `/api/v1/auth/register/step-2` | Kayıt tokenı | Avatar seçimi, kullanıcı adı ve kayıt SMS'i |
| POST | `/api/v1/auth/register/otp/verify` | Kayıt tokenı | Kayıt OTP doğrulaması ve access token |
| POST | `/api/v1/auth/register/otp/resend` | Kayıt tokenı | Kayıt OTP'sini yeniden gönderir |
| POST | `/api/v1/auth/login` | Yok | Telefon/PIN kontrolü ve giriş SMS'i |
| POST | `/api/v1/auth/login/otp/verify` | Login tokenı | Giriş OTP doğrulaması ve access token |
| POST | `/api/v1/auth/login/otp/resend` | Login tokenı | Giriş OTP'sini yeniden gönderir |
| POST | `/api/v1/auth/logout` | Müşteri Bearer | Access tokenı iptal eder |
| GET | `/api/v1/users` | Yok | Sayfalanmış public kullanıcı profilleri |
| GET | `/api/v1/users/me` | Müşteri Bearer | Oturum kullanıcısını getirir |
| DELETE | `/api/v1/users/me` | Müşteri Bearer | Kullanıcı adı ve PIN onayıyla hesabı siler |
| POST | `/api/v1/operator/login` | Yok | Operatör girişi |
| POST | `/api/v1/operator/logout` | Operatör Bearer | Operatör tokenını iptal eder |
| GET | `/api/v1/operator/users` | Operatör Bearer | Müşterileri yönetim bilgileriyle listeler |
| DELETE | `/api/v1/operator/users/{userId}` | Operatör Bearer | Müşteriyi kalıcı olarak siler |

Public kullanıcı listesi `limit` ve `offset` parametrelerini destekler. Telefon, PIN özeti ve oturum bilgileri public cevapta bulunmaz. Varsayılan limit 20, izin verilen en yüksek limit 100'dür.

## Yapılandırma

Tüm örnek değişkenler `.env.example` dosyasındadır.

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `APP_ENV` | `development` | `development`, `test` veya `production` çalışma ortamı |
| `HTTP_ADDR` | `:8080` | HTTP dinleme adresi |
| `MONGODB_URI` | Yok | Zorunlu MongoDB Atlas bağlantı adresi |
| `MONGODB_DATABASE` | `ceksepet` | Kullanılacak MongoDB veritabanı |
| `PUBLIC_BASE_URL` | `http://localhost:8080` | Public URL ve Swagger bağlantıları için temel adres |
| `TOKEN_SECRET` | Development değeri | Access token özetlerinde kullanılan en az 32 karakterlik production sırrı |
| `OTP_SECRET` | Development değeri | OTP özetlerinde kullanılan ayrı production sırrı |
| `OTP_WEBHOOK_URL` | Boş | Production'da HTTPS olması gereken SMS sağlayıcı webhook'u |
| `OTP_WEBHOOK_TOKEN` | Boş | SMS webhook yetkilendirme tokenı |
| `OPERATOR_USERNAME` | Boş | Test operatörü kullanıcı adı |
| `OPERATOR_PASSWORD` | Boş | Test operatörü parolası; kullanıcı adıyla birlikte tanımlanır |
| `OPERATOR_TOKEN_TTL` | `8h` | Operatör token geçerlilik süresi |
| `REGISTRATION_TTL` | `30m` | Tamamlanmamış kayıt geçerlilik süresi |
| `LOGIN_TTL` | `10m` | Tamamlanmamış giriş geçerlilik süresi |
| `OTP_TTL` | `4m` | OTP geçerlilik süresi |
| `OTP_RETRY_AFTER` | `60s` | Yeniden OTP gönderme bekleme süresi |
| `PIN_MAX_ATTEMPTS` | `5` | Kilitlenmeden önce izin verilen hatalı PIN sayısı |
| `PIN_ATTEMPT_WINDOW` | `15m` | Hatalı PIN sayacının zaman penceresi |
| `PIN_LOCK_DURATION` | `15m` | PIN kilit süresi |
| `CORS_ALLOWED_ORIGINS` | `*` | Virgülle ayrılmış izinli origin listesi |

Production ortamında `TOKEN_SECRET` ve `OTP_SECRET` en az 32 karakter olmalı, development örneklerini içermemeli ve OTP webhook adresi HTTPS olmalıdır. Operatör etkinse production operatör parolası da en az 32 karakter olmalıdır.

Gerçek `.env` dosyası Git'e eklenmemelidir. `.env.example` sürümlendiği için içindeki değerler repository erişimi olan herkes tarafından görülebilir; private repository tek başına secret yönetimi yerine geçmez. Production'da GitHub Actions Secrets veya deployment ortam değişkenleri kullanın ve daha önce paylaşılmış Atlas parolalarını kullanıma almadan önce değiştirin.

## MongoDB veri modeli

| Koleksiyon | İçerik |
|---|---|
| `users` | Profil, normalize telefon, kullanıcı adı, avatar ve bcrypt PIN özeti |
| `registration_sessions` | Tamamlanmamış kayıt adımları ve kayıt OTP özetleri |
| `login_sessions` | Telefon/PIN kontrolü sonrası geçici login OTP oturumları |
| `access_sessions` | Müşteri access token özeti, kullanıcı bağı ve iptal zamanı |
| `operator_sessions` | Süreli operatör token özeti ve iptal bilgileri |
| `login_attempts` | Telefon bazlı hatalı PIN sayacı ve kilit zamanı |

Telefon, kullanıcı adı ve token özetleri için unique indexler; geçici kayıt, login, operatör ve PIN deneme kayıtları için TTL indexleri oluşturulur. Süresi dolan kayıtlar MongoDB TTL mekanizmasına ek olarak uygulamanın dakikalık temizleme göreviyle de temizlenir.

Kullanıcı oluşturma + access session oluşturma ve login tamamlama + access session oluşturma işlemleri MongoDB transactionı içinde atomiktir. Hesap silme işlemi kullanıcıyı ve ilişkili oturum kayıtlarını birlikte kaldırır.

Veritabanı adı `ceksepet` olarak değiştirilmiştir. Önceki veritabanında oluşturulmuş veriler otomatik taşınmaz; gerekiyorsa Atlas üzerinde ayrıca migration yapılmalıdır.

## Mobil uygulama entegrasyon notları

- `registrationToken` ve `loginToken` kısa ömürlü geçici değerlerdir; Keychain, Keystore, UserDefaults veya SharedPreferences'a kalıcı yazılmamalıdır.
- Access token uygulama kapandığında kaybolmaması için iOS Keychain veya Android Keystore tabanlı güvenli depolamada tutulmalıdır.
- Backend uygulamanın işletim sistemi tarafından kapatıldığını güvenilir şekilde algılayamaz; tamamlanmamış kayıt ve login kayıtlarını TTL ile temizler.
- PIN, OTP ve access token loglara yazılmamalı; mobil trafik yalnızca HTTPS üzerinden gönderilmelidir.
- Altı haneli PIN düşük entropili olduğu için gateway seviyesinde IP ve cihaz bazlı ek rate limit uygulanmalıdır.
- Production SMS sağlayıcısı `OTP_WEBHOOK_URL` üzerinden entegre edilir. Development ortamında OTP yalnızca test kolaylığı için cevapta gösterilir.

Webhook'a gönderilen gövde:

```json
{
  "channel": "sms",
  "destination": "+905551112233",
  "code": "123456"
}
```

## Test, kalite kontrolü ve build

Tüm yerel kontrolleri çalıştırmak için:

```bash
make check
```

Bu komut sırasıyla format kontrolü, `go vet`, race detector ile testler ve `ceksepet-api` binary build işlemini çalıştırır.

Ayrı komutlar:

```bash
make test
make vet
make build
make clean
```

HTTP testleri kayıt → avatar/kullanıcı adı → kayıt OTP → access token → login OTP → yeni access token → logout akışını izole bellek deposuyla doğrular.

Gerçek MongoDB index ve TTL entegrasyon testini isteğe bağlı çalıştırmak için:

```bash
MONGODB_TEST_URI="$MONGODB_URI" go test ./internal/store -run TestMongoStore
```

Bu test benzersiz bir `ceksepet_test_*` veritabanı oluşturur ve test sonunda kaldırır. Production veritabanı adını test veritabanı olarak kullanmayın.

## Docker ile çalıştırma

Proje kökündeki `.env` hazırsa:

```bash
docker compose up --build
```

Arka planda başlatmak için:

```bash
docker compose up --build -d
```

Loglar ve durdurma:

```bash
docker compose logs -f api
docker compose down
```

Container içindeki uygulama `ceksepet-api` adıyla, yetkisiz `ceksepet` Linux kullanıcısı altında çalışır. Kalıcı veriler container dosya sistemine değil doğrudan MongoDB Atlas'a yazılır.

## Proje yapısı

```text
.
├── main.go                   # go run . için giriş noktası
├── cmd/api/                  # Önceki çalıştırma komutuyla uyumlu giriş noktası
├── docs/                     # İş akışı ve gereksinim notları
├── internal/app/             # Ortak başlangıç ve graceful shutdown
├── internal/config/          # .env yükleme ve yapılandırma doğrulama
├── internal/domain/          # Domain modelleri
├── internal/httpapi/         # Handler, middleware, Swagger ve OpenAPI
├── internal/security/        # Token, OTP, PIN ve güvenlik yardımcıları
├── internal/store/           # MongoDB repository ve transaction işlemleri
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── .env.example
```

## Sık karşılaşılan sorunlar

### `MONGODB_URI is required`

Proje kökünde `.env` bulunduğunu ve içinde boş olmayan `MONGODB_URI` tanımlandığını kontrol edin. Komutu alt klasörden çalıştırıyorsanız uygulama üst klasörlerdeki `.env` dosyasını da arar.

### MongoDB bağlantı zaman aşımı veya TLS hatası

- Atlas cluster'ının çalışır durumda olduğunu kontrol edin.
- Atlas **Network Access** listesine mevcut public IP adresini ekleyin.
- Database User kullanıcı adı/parolasını ve URI içindeki özel karakterlerin URL encoded olduğunu doğrulayın.
- VPN, kurumsal firewall veya DNS kısıtlarını kontrol edin.

### `bind: address already in use`

8080 portunu kullanan süreci bulun:

```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN
```

Gerekirse `.env` içindeki `HTTP_ADDR` değerini örneğin `:8081` olarak değiştirin.

Ayrıntılı ürün akışı kararları için [`docs/requirements.md`](docs/requirements.md) dosyasına bakın.
