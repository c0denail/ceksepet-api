# Görsellerden uygulanan ürün ve çekiliş API’leri

Bu belge paylaşılan altı ekranın backend karşılığını açıklar. Görsellerdeki örnek ürünler, 2345 gibi bakiyeler, `Other Tag` etiketleri ve yer tutucu açıklamalar gerçek veri olarak eklenmez. Kayıt/giriş akışı korunur. Yeni endpointlerin istek ve cevap şemaları `/swagger/index.html` içinde bulunur.

## Ekran → API eşlemesi

Tüm yollar `/api/v1` önekiyle kullanılır.

| Ekran/alan | API ve veri |
|---|---|
| Anasayfa aktif çekilişler | `GET /raffles`; varsayılan `status=active` |
| Marka veya ürün ara | `GET /raffles?q=Samsung`; başlık ve marka adı araması |
| Popüler / En Yeniler | `sort=popular` / `sort=newest`; popülerlik kullanılan toplam biletle sıralanır |
| Kategori / marka filtreleri | `GET /catalog`; `categoryId`, `brandId` liste parametreleri |
| Üst bardaki bilet bakiyesi | `GET /users/me/wallet` → `balance` |
| Çekilişler güvenli mi? / Hemen öğren | `GET /content/raffle-safety` |
| Dikey ve yatay ürün kartı | `RaffleCard`: başlık, marka, fotoğraflar, `soldTickets`, `quote.totalTickets`, `progressPercent` |
| Yeni / Bitmek üzere / Sonlandı | `isNew`, `badge`, `status`; yeni = yayından sonraki 7 gün, bitmek üzere = en az %80 dolu |
| Favori kalbi | `PUT /raffles/{id}/favorite`, `DELETE /raffles/{id}/favorite`; cevapta `isFavorite` |
| Favori listesi | `GET /users/me/favorites` |
| Katıldığım Çekilişler | `GET /users/me/participations`; aktif ve sonlananlar, kullanıcı için `myTickets` |
| Katılmadınız boş ekranı | Liste `items: []`, `total: 0`, `hasMore: false` döner |
| Detay fotoğraf galerisi | `GET /raffles/{id}` → `raffle.photoUrls`; yükleme sırası korunur |
| Satıcı adı, avatarı, puanı | Detayda `seller`, `sellerRating.average`, `sellerRating.count` |
| Satıcı puanı / Tümünü gör | `GET /sellers/{sellerId}/reviews` |
| Ürün açıklaması, marka, kategori, kargo | Detayda `raffle.description`, `brandName`, `categoryName`, `shippingPayer` |
| Soru-cevap önizlemesi | Detayda son 2 kök soru, her birinde son 2 yanıt ve `replyCount` |
| 28 Yorumu gör | `GET /raffles/{id}/questions`; `total` kök soru sayısıdır |
| Satıcıya soru sor | `POST /raffles/{id}/questions` |
| Yanıtla / Tüm Cevapları gör | `POST` / `GET /raffles/{id}/questions/{questionId}/replies` |
| Satıcı yanıtının ayrıştırılması | Soru/yanıt cevabında `isSeller: true` |
| Diğer Çekilişlere gözat | `GET /raffles/{id}/related`; aynı kategorideki aktif ürünler |
| Katıl butonu ve onay penceresi | `POST /raffles/{id}/participation-preview` |
| Onayla | `POST /raffles/{id}/participations`, `confirmed: true`, `Idempotency-Key` |
| Vazgeç | Ağ çağrısı gerekmez; önizleme bilet harcamaz ve rezervasyon oluşturmaz |
| Torba çekilişi nedir? | `GET /content/bag-raffle` |
| Fotoğraf ekle | `POST /photos`, multipart `file`; dönen `id` ürün isteğinde kullanılır |
| Başlık, açıklama, kategori, marka, kargo | `POST /raffles` gövdesindeki alanlar |
| Fiyat, bilet değeri, kazanç, komisyon, toplam bilet | `POST /raffles/quote`; ürün oluşturulurken yeniden hesaplanır |
| Form altındaki açıklama | `GET /content/selling` |
| Onaya Gönder | `POST /raffles` → `pending`; satıcı `GET /users/me/raffles` ile izler |
| Bildirim simgesi | `GET /users/me/notifications`, `unreadCount`; `PUT /users/me/notifications/{id}/read` |

Liste endpointleri `limit` (1–100; varsayılan 20) ve `offset` (0–1000000) kabul eder. Sonuçlar `items`, `total`, `limit`, `offset`, `hasMore` alanlarını içerir. Sıralama tarih + kimlik ile kararlıdır. Güncellenen listelerde offset sayfalama sırasında yeni kayıtlar nedeniyle kayma olabilir; yenilemede offset sıfırlanmalıdır.

## Fiyat ve bilet kuralı

Görseller komisyon formülünü belirtmediği için ayarlanabilir başlangıç değerleri kullanılır:

```dotenv
TICKET_VALUE_KURUS=100
COMMISSION_BPS=1000
```

100 kuruş = 1 TL = 1 bilet karşılığı; 1000 baz puan = %10 komisyon. Satıcının istediği fiyat kazanç hedefidir; komisyon buna eklenir. Kuruş ve bilet alanları JSON tam sayıdır. Kesirler yukarı yuvarlanır:

```text
sellerTickets     = ceil(priceKurus / ticketValueKurus)
commissionTickets = ceil(sellerTickets × commissionBps / 10000)
totalTickets      = sellerTickets + commissionTickets
```

2345 TL için `priceKurus: 234500` gönderilir; 2345 kazanç bileti + 235 komisyon bileti = 2580 toplam bilet döner. Ekrandaki tutarsız örnek sayılar sabitlenmez. Satıcının tutarı ve oranlar ürün üzerinde saklanır; sonradan ortam ayarlarının değişmesi mevcut ürünleri yeniden fiyatlandırmaz. `sellerTickets` tahmini kazançtır; bu API satıcıya otomatik ödeme yapmaz.

## Swagger ile tam akış

1. Mevcut kayıt/SMS akışıyla bir satıcı ve bir katılımcı hesabı açın. Her birinin access tokenını saklayın.
2. Satıcı tokenıyla `POST /photos` çağırıp 1–5 fotoğraf yükleyin. Tek çağrı tek fotoğraf kabul eder.
3. `POST /raffles/quote` ile fiyatı önizleyin:

```json
{"priceKurus": 234500}
```

4. Satıcı tokenıyla `POST /raffles` çağırın:

```json
{
  "title": "Samsung Galaxy S Tablet",
  "description": "Kutusu ve tüm aksesuarları ile temiz tablet.",
  "categoryId": "tablet",
  "brandId": "samsung",
  "shippingPayer": "buyer",
  "photoIds": ["YUKLEMEDEN_DONEN_ID"],
  "priceKurus": 234500
}
```

5. Operatör tokenıyla `GET /operator/raffles` listesinden ürünü bulun. Fotoğrafı operatör tokenıyla görüntüleyebilirsiniz. `POST /operator/raffles/{id}/moderation` çağırın:

```json
{"decision": "approve"}
```

Ret için `{"decision":"reject","reason":"Fotoğraf net değil."}` kullanılır. Yalnızca `pending` ürünler incelenebilir. Onay/ret satıcının bildirim listesine yazılır.

6. Yeni hesaplar sıfır biletle başlar. Test için operatör tokenıyla `POST /operator/users/{userId}/tickets`, `Idempotency-Key: credit-test-0001` başlığı ve aşağıdaki gövdeyi gönderin:

```json
{"tickets": 1000, "reason": "Mobil akış testi"}
```

7. Katılımcı tokenıyla `POST /raffles/{id}/participation-preview` çağırın:

```json
{"tickets": 200}
```

8. Kullanıcı Onayla düğmesine bastığında `POST /raffles/{id}/participations`, `Idempotency-Key: join-mobile-0001` ve aşağıdaki gövdeyi gönderin:

```json
{"tickets": 200, "confirmed": true}
```

İlk başarı `201`, aynı işlemin tekrarı `200` ve `replayed: true` döndürür. Yeniden denemede **aynı anahtarı ve gövdeyi** kullanın. Yeni, bilinçli bir katılım için yeni anahtar üretin. Makbuzdaki `balanceAfter` işlem anındaki bakiyedir; güncel bakiye için cüzdanı yenileyin. Önizleme ile onay arasında kapasite/bakiye değişirse onay `409` dönebilir.

## Saklama ve erişim

- Yeni MongoDB koleksiyonları: `raffles`, `photos`, `favorites`, `participations`, `ticket_entries`, `questions`, `reviews`, `notifications`.
- `users.ticket_balance` mevcut kullanıcılar için eksikse sıfır okunur. İlk kredi bu alanı oluşturur; manuel migration gerekmez.
- Fotoğraflar her biri en fazla 5 MiB olacak şekilde ayrı MongoDB belgelerinde saklanır. Böylece container yeniden başlatıldığında kaybolmaz. JPEG/PNG içeriği ve boyutları sunucuda doğrulanır. Fotoğraf kimlikleri tek ürüne, yalnızca sahibi tarafından bağlanabilir.
- Onaysız/reddedilen ürünler ve fotoğraflar public listelerde görünmez. Fotoğrafın sahibi veya operatör inceleyebilir. Aktif/tamamlanmış ürünlerin fotoğrafları public erişime açılır.
- Public çekilişlerde token isteğe bağlıdır. Token verilirse geçerli olmalıdır; hatalı token sessizce ziyaretçiye dönüştürülmez. Telefon, PIN ve cüzdan bilgileri public detayda gösterilmez.
- Katılımda cüzdan, kapasite, katılım özeti ve işlem makbuzu tek transaction içindedir. MongoDB replica set / Atlas gerekir. Tek sunuculu replica set yerel test için yeterlidir.
- Katılım ve kredi istekleri kullanıcı + idempotency anahtarıyla benzersizdir. Aynı anahtar farklı miktar, ürün veya işlem türüyle kullanılamaz. Başarısız işlem bilet tüketmez; başarısız anahtar daha sonra tekrar denenebilir.
- Satıcı kendi ürününe katılamaz. Kendi hesabına kullanıcı tokenıyla bilet yüklemek mümkün değildir. Manuel kredi ayrı operatör tokenı, gerekçe ve işlem kaydı gerektirir.
- Aktif/tamamlanmış ürün veya bilet işlem kaydı olan hesap silinmez; `409 ACCOUNT_HAS_MARKETPLACE_RECORDS` döner. Finansal kayıtların sahipsiz kalmaması için hesap kapatma/hesaplaşma akışı tamamlanana kadar destek gerekir. Yalnızca bekleyen/reddedilen ürünleri bulunan hesap silinebilir; ürünleri, fotoğrafları ve kişisel bildirimleri temizlenir.

## Görsellerde tanımlanmayan kurallar

- Toplam bilet dolunca durum `completed` olur ve kart `Sonlandı` gösterir. **Kazanan seçimi, teslimat ve satıcıya ödeme** bu ekranlarda tanımlanmadığından bu sürümde gerçekleştirilmez. Aktif/bekleyen çekilişin operatör tarafından iptali ve bilet iadesi [profil API rehberinde](profile-api.md) açıklanmıştır.
- Bilet satın alma/ödeme sağlayıcısı eklenmedi; operatör kredisi yalnızca yetkili manuel/test işlemdir.
- Değerlendirme başlangıç kuralı: sonlanan çekilişe katılan kullanıcı çekiliş başına bir kez 1–5 arasında, 0.5 adımlı puan verebilir. Bu, doğrulanmış teslimat değerlendirmesi değildir. Henüz değerlendirme yoksa ortalama ve sayı sıfırdır.
- Kategori/marka kataloğu `internal/domain/catalog.go`, Türkçe yardım metinleri `internal/httpapi/marketplace.go` içindedir. Bunlar başlangıç kataloğudur; gerçek katalog ve editoryal metinler burada değiştirilebilir. `Other Tag` görsel yer tutucusudur.
- Bildirimler uygulama içidir; push bildirim sağlayıcısı bağlı değildir. Ürün onayı/reddi ve yeni soru/yanıt olaylarında kayıt oluşturulur.

## Doğrulama

```bash
go test ./...
go vet ./...
MONGODB_TEST_URI='mongodb://127.0.0.1:27029/?replicaSet=ceksepet_test_rs' go test ./... -count=1
```

MongoDB testleri benzersiz `ceksepet_test_*` / `ceksepet_http_test_*` veritabanı oluşturup yalnızca kendi veritabanlarını kaldırır. URI verilmediğinde bu testler atlanır. Testler HTTP ekran akışlarını, yetki ayrımını, boş listeleri, hesaplama/yuvarlamayı, işlemlerin geri alınmasını, eşzamanlı son bilet kullanımını ve aynı anahtarla mükerrer istekleri kapsar.

MongoDB testlerinin çalışıp çalışmadığını `go test -v ./...` çıktısındaki PASS/SKIP satırlarından kontrol edin. `go test -race` Windows'ta ayrıca uyumlu bir C derleyicisi gerektirir.
