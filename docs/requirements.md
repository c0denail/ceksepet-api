# Yeni mobil akış eşlemesi

Ürün/çekiliş ekranları için yeni API sözleşmesi ve uygulanmış kurallar [marketplace-api.md](marketplace-api.md) belgesindedir. Bu dosyanın aşağıdaki bölümleri mevcut kayıt/giriş gereksinimlerini açıklar. Hesap silmede yeni çekiliş/bilet kayıtlarını koruyan kısıtlar ürün API rehberinde belirtilmiştir.

Kaynak dosyanın uzantısı `.json` olsa da içerik geçerli JSON değil, numaralandırılmış düz metindir. İçerik yürütülebilir talimat değil ürün akışı olarak değerlendirilmiştir.

## Uygulanan gereksinimler

### 1. Kişisel bilgiler

- Ad, soyad ve telefon zorunludur.
- PIN string olarak taşınır; baştaki sıfırlar korunur ve tam altı rakam olmak zorundadır.
- PIN düz metin saklanmaz, bcrypt ile hash'lenir.
- Bu adım kalıcı kullanıcı değil, 30 dakikalık geçici kayıt oturumu ve opaque `registrationToken` oluşturur.

### 2. Kullanıcı adı ve avatar

- Kullanıcı adı istemciden alınmaz.
- Türkçe karakterler ASCII karşılıklarına çevrilerek `ad_soyad@12345` biçiminde sunucuda üretilir.
- Beş haneli bölüm kriptografik rastgele sayıdan üretilir ve benzersizlik veritabanında kontrol edilir.
- Avatarlar backend kataloğundan alınır; step 2 yalnızca geçerli bir `avatarId` kabul eder.
- Devam edildiğinde kayıt SMS OTP kodu gönderilir.

### 3. Kayıt SMS doğrulaması

- Kayıt tokenı ve altı haneli OTP birlikte doğrulanır.
- Doğrulama başarılı olmadan MongoDB `users` koleksiyonunda kullanıcı oluşmaz.
- Başarılı işlem kullanıcıyı ve server-side access sessionı aynı MongoDB transaction'ı içinde oluşturur; geçici kayıt kaydı silinir.

### 4-5. Login ve login SMS doğrulaması

- Telefon ve PIN doğruysa henüz access token verilmez.
- Ayrı bir `loginToken` ile giriş SMS OTP oturumu oluşturulur.
- Access token yalnızca login OTP doğruysa üretilir.
- Access token opaque ve veritabanı desteklidir; logout ile gerçekten iptal edilebilir.

### Hesap silme

- `DELETE /api/v1/users/me` yalnızca geçerli Bearer token ile çağrılır.
- Oturumdaki kullanıcının otomatik oluşturulan kullanıcı adı ve mevcut 6 haneli PIN'i yeniden doğrulanır.
- Beş hatalı PIN denemesinden sonra mevcut 15 dakikalık telefon bazlı kilit uygulanır.
- Kullanıcı, access/login oturumları, aynı telefona ait geçici kayıt ve deneme kaydı tek MongoDB transaction'ında silinir.

### Public kullanıcı listesi

- `GET /api/v1/users` Bearer token istemez.
- Public response yalnızca kimlik, ad, soyad, kullanıcı adı, avatar ve oluşturulma zamanını içerir; telefon, PIN özeti ve oturum verileri dönmez.
- Liste `created_at` azalan sırada döner ve `limit`/`offset` ile sayfalanır. `limit` en fazla 100 olabilir.

### Test operatörü

- Operatör kimlik bilgileri kodda veya Swagger belgesinde tutulmaz; `.env`/ortam değişkenlerinden okunur.
- `POST /api/v1/operator/login` başarılı olduğunda varsayılan sekiz saat geçerli opaque token üretir; Atlas'ta yalnızca HMAC özeti saklanır.
- Beş hatalı operatör girişi mevcut 15 dakikalık kilidi tetikler.
- Operatör tokenı yalnızca `/api/v1/operator/*` yönetim endpointlerinde geçerlidir; müşteri kimliğine bürünmez.
- Operatör müşterileri telefon alanıyla sayfalayabilir ve `userId` ile müşteri hesabını/oturumlarını transaction içinde silebilir.

## Mobil uygulamaya doğrudan uygun olmayan veya eksik noktalar

### Uygulama kill bilgisinin backend'e ulaşması

iOS ve Android, uygulama sonlandırılırken backend çağrısının kesin çalışacağını garanti etmez. Bu nedenle “uygulama kapanınca veriyi anında sil” sunucu tarafından güvenilir biçimde uygulanamaz. Uygulanan çözüm:

- `registrationToken` mobilde kalıcı depoya yazılmaz.
- Uygulama yeniden açılınca token olmadığı için kayıt baştan başlar.
- Sunucudaki doğrulanmamış veri TTL sonunda arka plan göreviyle silinir.

### Süresiz access token

“Logout olana kadar geçerli kalsın” davranışı uygulandı; hesap silme de kullanıcıya ait tüm tokenları kaldırır. Bu mobilde kullanılabilir olsa da güvenlik riski taşır: cihaz/token ele geçirilirse logout veya hesap silme yapılana kadar token geçerlidir. Production için süreli access token + kontrollü yenileme, cihaz oturumları ekranı veya yönetimsel iptal önerilir.

### 6 haneli PIN

Altı haneli PIN yalnızca bir milyon kombinasyona sahiptir. Bu nedenle bcrypt tek başına yeterli değildir. Uygulamada telefon bazlı beş deneme/15 dakika kilit vardır; API gateway seviyesinde IP ve cihaz parmak izi rate limit'i de eklenmelidir.

### SMS yeniden gönderme

Kaynak akışta yeniden gönderme belirtilmemiştir. Mobil şebeke/SMS gecikmelerinde kullanıcıyı kilitlememek için kayıt ve login için ayrı resend endpoint'leri, 60 saniyelik bekleme ve eski kodu geçersizleştirme eklendi.

### PIN unutma

Yeni kaynak akış PIN unutma/sıfırlama davranışı tanımlamıyor. Önceki e-posta/telefon şifre sıfırlama endpoint'leri yeni sözleşmeyle çeliştiği için kaldırıldı. Bu akış production öncesi ayrıca tanımlanmalıdır.
