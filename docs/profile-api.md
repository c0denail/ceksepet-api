# Profil ekranları API rehberi

Bu rehber son paylaşılan yedi görselin alanlarını mevcut Go API'sine eşler. İstekler `/api/v1` altındadır. Oturum gerektiren işlemler `Authorization: Bearer <accessToken>` kullanır. Swagger: `/swagger/index.html`.

## Ekran ve alan eşlemesi

| Ekran | Endpoint | Kullanılacak alanlar |
|---|---|---|
| Profil başlığı | `GET /users/me` | `displayName`, `firstName`, `lastName`, `avatarUrl`, `ticketBalance` |
| Düzenlediğim çekilişler | `GET /users/me/raffles` | Sayfalı ürün kartları ve durum filtresi |
| Katıldığım çekilişler | `GET /users/me/participations` | Aynı kartlar; ayrıca `myTickets`, `myRefundedTickets` |
| Favorilerim | `GET /users/me/favorites` | Ürün fotoğrafı, başlık, marka, ilerleme, favori durumu |
| Kalp düğmesi | `PUT /raffles/{raffleId}/favorite`, `DELETE /raffles/{raffleId}/favorite` | `isFavorite`; tekrar çağrılabilir |
| Cüzdan bakiyesi | `GET /users/me/wallet` | `balance`, `ticketValueKurus`, `currency` |
| Cüzdan hareketleri | `GET /users/me/wallet/transactions` | Bilet yükleme, katılım ve iptal iadesi kayıtları |
| Torba / SSS | `GET /faqs` | `title`, `aboutTitle`, `about`, sıralı `items[].id/question/answer` |
| Satıcı profil başlığı | `GET /sellers/{sellerId}` | `seller.displayName/avatarUrl`, `rating.average/count`, `raffleCount`, `canReport` |
| Satıcının düzenlediği çekilişler | `GET /sellers/{sellerId}/raffles` | Sayfalı ürün kartları |
| Satıcının yorumları | `GET /sellers/{sellerId}/reviews` | `items[].author.displayName/avatarUrl`, `rating`, `body`, `createdAt`; `averageRating`, `total` |
| Değerlendirme gönderme | `POST /raffles/{raffleId}/reviews` | `rating`, `body` |
| Şikâyet nedeni seçenekleri | `GET /seller-report-reasons` | `items[].id/label`, `maxDescriptionLength` |
| Şikâyet gönderme | `POST /sellers/{sellerId}/reports` | `reasonId`, isteğe bağlı `description` |
| Çıkış Yap | `POST /auth/logout` | Mevcut sunucu oturumunu iptal eder; istemci tokenı temizler |

Ürün kartlarında `photoUrls[0]`, `title`, `brandName`, `soldTickets`, `quote.totalTickets`, `progressPercent`, `isFavorite`, `status` ve `badge` bulunur. İlerleme çubuğunu `progressPercent`, altındaki sayıları `soldTickets/quote.totalTickets` ile gösterin. `createdAt` UTC ISO 8601 tarihidir; “1 gün önce” metnini mobil uygulama üretir.

Logo, menü ikonları, geri/kapat düğmeleri, sekme seçimi ve SSS'nin açılıp kapanması istemciye aittir. Görselde yalnızca simgesi bulunan Ayarlar için yeni alanlar veya ayar endpointleri eklenmedi.

## Çekiliş filtreleri

| Görseldeki seçenek | `status` |
|---|---|
| Tümü | `all` |
| Devam Edenler | `active` |
| Sonlananlar | `completed` |
| İptal Edilenler | `cancelled` |

- Düzenlediğim listesinde varsayılan `all`; bekleyen (`pending`) ve reddedilen (`rejected`) ürünler de bu kullanıcıya görünür.
- Katıldığım listesinde parametresiz veya `all` istek, aktif/sonlanan/iptal edilen katılımları getirir.
- Satıcı profilindeki public liste yalnızca aktif ve sonlanan çekilişleri gösterir. `status=all|active|completed` kabul eder. Satıcı kendi tokenıyla çağırsa da bekleyen/reddedilen ürünler public listeye girmez.
- Listeler `limit` (varsayılan 20, en fazla 100), `offset` (varsayılan 0) destekler. Çekiliş listelerinde `q`, `categoryId`, `brandId`, `sort=newest|popular` da kullanılabilir.
- Liste cevapları `items`, `total`, `limit`, `offset`, `hasMore` döndürür. Boş ekran için `items: []` ve `total: 0` yeterlidir.

## Satıcı profili ve değerlendirmeler

Profil, satıcının çekilişleri ve yorumları tokensız okunabilir. Token gönderildiğinde geçerli müşteri tokenı olmalıdır; kartlardaki favori/katılım alanları çağıran kullanıcıya göre döner. Telefon ve cüzdan bakiyesi public satıcı cevabında bulunmaz.

Değerlendirme puanı 1–5 arasında, 0.5 adımlıdır; görseldeki 4.5 desteklenir. Yorum 1–2000 karakterdir. Sonlanan çekilişe katılan kullanıcı, çekiliş başına bir kez değerlendirebilir. Kendi çekilişini, katılmadığı çekilişi veya iptal edilen çekilişi değerlendiremez. Bu değerlendirme kuralı teslimat doğrulaması anlamına gelmez.

```json
{"rating":4.5,"body":"Satıcının iletişiminden memnun kaldım."}
```

Değerlendirme yoksa ortalama ve sayı sıfırdır. `rating.count` toplam değerlendirme sayısını, `raffleCount` satıcının yayınlanmış aktif ve sonlanan çekilişlerinin toplamını gösterir.

## Satıcı şikâyeti

Görselde neden metinleri yer tutucu olduğu için başlangıç seçenekleri tanımlandı:

- `misleading_listing`: Yanıltıcı ürün veya satıcı bilgisi.
- `inappropriate_behavior`: Uygunsuz davranış veya iletişim.
- `suspected_fraud`: Dolandırıcılık şüphesi.

İstemci metinleri neden kataloğundan almalı ve tek bir `reasonId` göndermelidir. Açıklama isteğe bağlıdır, kırpıldıktan sonra en fazla 2000 Unicode karakter olabilir.

```json
{"reasonId":"misleading_listing","description":"Ürün açıklamasındaki bilgi yanıltıcı."}
```

Başarılı gönderim `201` ve `status: "open"` döndürür. Kendini şikâyet etmek `403`, bulunmayan satıcı `404`, aynı kişi/satıcı için ikinci açık şikâyet `409` döndürür. Neden kataloğu herkese açıktır; gönderim müşteri oturumu gerektirir.

Şikâyetlerin işlenmesi için mevcut operatör yetkisiyle:

- `GET /operator/seller-reports?status=open&limit=20&offset=0`: kuyruk; `open|resolved|dismissed|all`.
- `POST /operator/seller-reports/{reportId}/resolution`: `{"status":"resolved","resolution":"Satıcı bilgisi kontrol edilip düzeltildi."}`. Durum `resolved|dismissed`, sonuç açıklaması 3–1000 karakter.

Şikâyetler satıcıya veya diğer müşterilere listelenmez. Sonuç tekrar gönderilirse aynı kayıt döner; farklı bir sonuçla üzerine yazmak `409` verir. Önceki şikâyet sonuçlandıktan sonra yeni bir şikâyet gönderilebilir.

## Çekiliş iptali ve bilet iadesi

`POST /operator/raffles/{raffleId}/cancel`, operatör oturumuyla `{"reason":"Ürün temin edilemiyor."}` alır. Gerekçe 3–1000 karakterdir. Müşteri veya satıcı tokenı bu işlemi yapamaz.

Yalnızca bekleyen/aktif çekiliş iptal edilebilir. İptal, katılımcıların kullandığı biletlerin tamamının cüzdana iadesi, `raffle_refund` hareketleri ve bildirimler aynı MongoDB transactionında kaydedilir. Herhangi bir adım başarısız olursa tüm işlem geri alınır. Tekrar iptal ikinci iade oluşturmaz; ilk iptal gerekçesi korunur.

`soldTickets` geçmiş katılım sayısını korur. İptal kartında `remainingTickets: 0`, `badge: "İptal edildi"`, `cancelledAt`, `cancellationReason` bulunur. Katılımcının kartındaki `myRefundedTickets` iade edilen sayıdır. Yeni katılım kapatılır. Önceden yayınlanmış ürünün detay/fotoğrafları geçmiş ekranlarından okunabilir; hiç yayınlanmadan iptal edilen ürün public açılmaz.

Sonlanmış/reddedilmiş ürünün iptali `409` döndürür. Katılım geçmişi bulunan satıcı hesapları, iade sonrası da finansal kayıtları koruyan mevcut hesap kapatma kuralına tabidir.

## Cüzdana kartla yükleme

Ödeme sağlayıcısı henüz seçilmediği için kullanıcının isteği doğrultusunda **kartla yükleme entegrasyonu bu çalışmaya dahil edilmedi**. 50/100/150 seçimi, özel miktar, kart bilgileri, tahsilat ve ödeme onayı için yeni API eklenmedi. Kart numarası/CVC alınmaz veya saklanmaz; mobil tarafta “Bilet Yükle” işlemi entegrasyon tamamlanana kadar etkinleştirilmemelidir.

Mevcut cüzdan bakiyesi/hareketleri kullanılabilir. Görseldeki 1020/2600 ifadesinden varsayımsal bir cüzdan limiti türetilmedi; gerçek bakiye tek bir tam sayıdır. Operatörün mevcut manuel bilet kredisi kartla ödeme değildir.

## Doğrulama

`go test ./...` ve `go vet ./...` normal kontrollerdir. HTTP testleri profil alanları ve gizlilik, public satıcı listeleri, yarım puan, şikâyet yetkileri/tekilleştirme, durum filtreleri ve bir kez iade akışlarını kapsar.

Gerçek MongoDB testleri yalnızca ayrı bir replica set/Atlas test bağlantısı `MONGODB_TEST_URI` ile verilirse çalışır. Benzersiz test veritabanları oluşturur ve kendi veritabanlarını temizler. Yeni entegrasyon senaryoları eşzamanlı iptal/katılım, çift iptal, hata halinde transaction rollback ve açık şikâyet unique indexini kapsar. Bağlantı verilmezse bu testler atlanır.
