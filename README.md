# Support Center

以 Go SSR、templ、HTMX、Tailwind CSS、PostgreSQL 和 Auth0 建立的輕量客服單系統。附件存放在 VPS 的私有 Docker volume。介面採方正設計，支援手機，預設以 `prefers-color-scheme` 自動跟隨系統深淺色；右上角的小型太陽／月亮按鈕顯示目前外觀，可手動切換並在同一瀏覽器記住選擇。網站名稱、Logo、圖示與頁尾版權由 `.env` 設定（見[網站名稱與 Logo](#網站名稱與-logo)），未設定時只顯示「支援中心」與內建圖示。客服單時間依瀏覽器時區顯示，未啟用 JavaScript 時明確顯示 UTC。介面採中性灰階，只保留客服單操作所需的文字。

## 已實作

- 公告：管理員在「管理設定 → 公告」新增、編輯、發布、停用或刪除公告；登入後各頁最上方同時顯示多則已發布公告，新公告排在前面。支援藍色資訊、綠色完成與紅色重要三種底色及圖示，深淺色皆可辨識。標題最多 120 字、內容 2,000 字，最多保留 20 則；內容以純文字呈現，不對外公開或寄信。

- 客戶：建立、依主旨或客服單編號（可不加 `#`）搜尋、篩選與分頁查看自己的客服單，回覆、附檔、結案及重新開啟。建立時可選一般、帳務或技術支援；優先級為黃色「標準」或紅色「緊急」。列表先分「未完成」（新進、處理中、待客戶、待外部）與「已完成」（已解決、已結案），再依細狀態篩選；數量會跟著搜尋條件更新。
- 參與者：開單時可填入最多 20 位參與者的 Email（逗號分隔）；開單者也可在建立後繼續邀請或移除。參與者一律只顯示 Email，不顯示帳號名稱或頭像；不論對方是否已註冊，邀請的回應、訊息和狀態碼都相同，無法藉由邀請得知某個 Email 是否已註冊。邀請已驗證帳號或由管理員授權的客服／管理員帳號會立即加入，權限綁定該帳號，不需接受邀請；尚無帳號的地址以相同且已驗證的 Email 登入即可存取。受邀參與者可自行退出，退出後不再有該單的使用者存取權限或站內通知；開單者不能自行退出。設定 SMTP 後，受邀的 Email 會收到邀請信。
- 客服／管理員：查看全部客服單，依狀態、優先級與負責人（指派給我／未指派）篩選，回覆、撰寫內部備註、調整狀態、優先級與問題類別，並以按鈕指派多位客服。類別變更會出現在對話記錄，客戶也看得到。
- 後台狀態流程：尚未受理、處理中、等待使用者回覆、等待其他單位回覆、已解決、已結案。客戶看不到內部「等待其他單位回覆」分類，會看到「處理中」。客戶回覆 → 處理中；客服公開回覆 → 等待使用者回覆；內部備註不變更公開狀態或時間。
- 自動結案：在「管理設定 → 自動訊息」設定已解決後 1–365 天結案，0 表示停用（預設）。到期檢查每分鐘執行；公開回覆或重新開啟會取消倒數，內部備註及更換負責人不影響期限。客服單的開單者及已加入的受邀使用者會收到已解決與自動結案的站內通知，包含以使用者身分參與的管理員及客服；手動結案也會新增「已結案」通知，已讀及未讀的歷史通知均保留，客服指派及其他後台管理事項不產生站內通知；管理員的通知頁也只顯示自己作為使用者參與的客服單。右上鈴鐺可查看、開啟或標為已讀，開啟時進入一般使用者的客服單頁面；客服單也顯示預計結案時間。變更天數套用目前已解決的客服單。首次安裝時，既有已解決客服單從安裝時間開始計算，保留完整寬限期。已有解決通知但缺少手動結案通知的客服單，會依最新結案紀錄補回通知並重查參與權限。
- 管理員在單一「管理設定」選單切換公告、AI 指令、自動訊息、Discord 通知、Email 通知、使用者與客服分組。可把客服加入一般、帳務、技術部門；指派按鈕依部門呈現，其他部門仍可選，方便修正客戶選錯類別。自動訊息可設定開單訊息、非服務時間訊息、臨時休假期間的優先回覆、時區，以及週一至週日各自的服務時段（每天最多 8 段）。各日未涵蓋的時間（例如午休）會套用非服務時間規則。臨時休假與非服務時間僅顯示於回覆框上方；沒有這些提示時，開單可送出一則標明「自動回覆」的一般開單訊息。休假結束後自動恢復原規則，預設不啟用。
- 技術類別的新客服單、轉入技術類別，以及指派技術部門成員，會為已驗證電子郵件的技術部門客服建立通知。設定 SMTP 後由背景工作寄送；未設定時暫存最多 7 天，不會假裝已寄出。詳見 [通知設定](docs/notifications.md)。
- Email 通知：設定 SMTP 後，真人客服或 AI 公開回覆時寄信給開單者和已加入的參與者（需已驗證 Email），回覆後等 2 分鐘再寄並合併期間的多則回覆，對方已回覆就不寄；邀請參與者時寄信給受邀的 Email，不論是否已註冊，信中只有邀請人 Email 與單號、不含客服單內容，並限制每人每天 30 封、每個地址每天 3 封。寄件人名稱為「`SITE_NAME` 支援中心」，連結回到 `APP_URL`，同時提供純文字與 HTML。管理員在「管理設定 → Email 通知」開關各類信件、選擇是否附上回覆內容並寄測試信；每個人可在「個人設定」關閉自己的通知。詳見 [通知設定](docs/notifications.md)。
- 帳號合併：以 SSO 登入且 Email 已驗證時，會接手 Portal 先前以同一 Email 建立的帳號，兩邊看到同一份客服單。
- Discord 通知：管理員在「管理設定 → Discord 通知」貼上 Discord 頻道的 Webhook 網址，可分別開關「新客服單」與「客戶回覆」（開單者與參與者的公開回覆；不含客服回覆、內部備註和自動訊息），以及是否附上訊息前 200 字的摘要。每則通知含單號、主旨、客戶名稱、部門、優先級（緊急為紅色）、後台連結，底部標示網站名稱。事件與客服單／回覆在同一個資料庫交易內排入佇列，背景工作每次最多合併 10 則送出，遵守 Discord 的頻率限制，失敗時退避重試，超過 1 天的事件不再傳送；Webhook 被刪除時會在設定頁顯示原因，並可傳送測試訊息。只接受 `discord.com`／`discordapp.com` 的 Webhook 網址，儲存後不再顯示 token；客戶文字的 Markdown 會跳脫，且不會觸發 @everyone 或其他提及。
- 客戶回覆輸入框上方顯示當前目前非客服服務時間或臨時休假訊息，使用後台設定的原文與時區，休假優先；恢復服務或停用規則後消失。AI 可繼續回覆，已解決及已結案時不顯示。提示不會寫入對話；符合目前非服務時間／休假文案的舊自動訊息會標記並隱藏，保留原始資料，一般開單及自動結案訊息仍顯示。
- 可選的 AI 回覆使用 OpenAI 官方 API 的 `gpt-6-luna`（可用 `OPENAI_MODEL` 調整）。管理員可依一般、帳務、技術支援設定完整的系統指令；空白欄位不傳送系統指令。AI 處理中與已回覆有不同狀態標示；回覆後，擁有者可確認「已解決」或轉真人。未轉真人時，使用者繼續回覆仍由 AI 接續處理。背景工作會重試失敗回覆，三次失敗後交回真人；使用者或客服也可立即轉真人。需設定金鑰及啟用開關，詳見 [AI 整合說明](docs/ai-assistance.md)。
- 私有附件：每次最多 3 個、每個 10 MiB，整個 request 最多 32 MiB；JPG、PNG、WebP、TXT、LOG、PDF、ZIP。
- 選取圖片後在瀏覽器直接顯示 WebP 預覽；圖片處理程式只在選取圖片時載入。PNG 先以 OxiPNG 無損最佳化，再與無損 WebP 比較，只上傳較小的檔案；JPEG／WebP 無法安全縮小時保留原檔。伺服器另外產生最高邊 1600px 的 WebP 預覽，點擊客服單縮圖可在同頁浮層放大，並可另開預覽或下載原檔。瀏覽器轉為 WebP 時保留解碼後的像素，原始 EXIF 等中繼資料不保證保留；預覽為較小的瀏覽用衍生圖。
- 已登入使用者和客服單訊息作者固定使用 Gravatar。VPS 以標準化 email 的 SHA-256 取得圖片並限量快取；瀏覽器只連回本站，沒有頭像或外部服務不可用時顯示姓名首字。
- 個人設定可查看自己的登入與使用紀錄及來源 IP、瀏覽器、系統資訊。只有管理員可設定簽名檔，公開回覆自動附上，內部備註不附加。管理員也可修改自己的顯示名稱，名稱不會被後續 Auth0 登入覆蓋，並可在後台搜尋使用者及查看最近登入時間。登入紀錄從功能上線後累積；舊操作當時未保存 IP／瀏覽器，會標示未記錄。
- Auth0 Authorization Code + PKCE、state、nonce、ID token 驗證，PostgreSQL 保存 24 小時 session。瀏覽器只持有隨機 session cookie，資料庫僅存 token 的 SHA-256。
- CSRF、伺服器端權限、SQL 參數化、templ HTML escaping、CSP 與私密頁面 `no-store`。
- 建立／回覆使用提交識別碼避免網路重試重複寫入；狀態、優先級與指派以版本檢查防止覆蓋別人的修改。
- 客服可查看最近 20 筆狀態、優先級及指派操作紀錄；資料庫保留完整紀錄。
- 資料庫遷移逐檔記錄 checksum，禁止悄悄改寫已套用的遷移。
- 表單在無 JavaScript 時也可操作；HTMX 提供列表篩選與回覆的局部更新。
- 介面支援繁體中文和英文，首次開啟依瀏覽器語言選擇；登入後在「個人設定」用國旗標示的語言按鈕手動切換，並以 cookie 記住一年。客服單內容、使用者名稱，以及管理員自訂訊息與 AI 指令維持原文。

目前沒有知識庫或即時聊天。未設定 SMTP 時不寄任何信，客戶須回到支援中心查看回覆。Auth0 用於登入，Gravatar 用於頭像；其餘頁面資源、客服單、session 與附件均由自己的 VPS 提供。公開 HTTPS 由你管理的 Cloudflare Tunnel 提供，建置時需要下載依賴。

## 功能驗收（可實際寫入，不需要 Auth0 租戶）

`cmd/preview` **只是唯讀畫面，不能用來驗收建立／回覆／附件等功能，也不是正式伺服器**。

在 VPS 終端執行：

```sh
bash scripts/start-acceptance.sh
```

初始的後端驗收只需要 Docker Compose 和 OpenSSL。腳本會建立獨立 Compose 專案 `supportcenter-acceptance`、`support_acceptance` 資料庫和私有附件 volume，產生僅存於 `.env.acceptance` 的隨機驗收密碼，並執行 `go test -race` 的真實 PostgreSQL 整合測試。專案不會安裝或啟動 Cloudflare Tunnel。

要從外部驗收，在 `.env.acceptance` 將 `APP_URL` 改成你自己管理的 HTTPS 網址，將你的 HTTPS 反向代理或 Tunnel 指向 VPS 的 `http://127.0.0.1:8082`，再執行 `bash scripts/start-acceptance.sh`。腳本會啟動驗收服務；公開網址可連通時，還會安裝鎖定的 npm 依賴及 Chromium，執行完整瀏覽器流程。若外部連線尚未就緒，驗收服務仍會留在本機連接埠，設定好入口後重跑腳本即可。若 Chromium 報缺少系統函式庫，執行 `npx playwright install --with-deps chromium` 後重跑。

驗收執行檔使用**同一套正式客服單 handler、SQL、權限檢查及附件儲存**，只有登入替換為驗收密碼；可用客戶 A、客戶 B、客服三種身分操作。資料會跨容器重啟保留。

驗收密碼可切換所有驗收身分，請只交給受信任測試者，不要接收正式客戶或真實敏感資料。正式 `Dockerfile` 只安裝 `server` 和 `admin`，沒有驗收登入程式；`cmd/server` 仍強制配置 Auth0。驗收資料庫名稱有程式檢查，防止誤接正式 database。

要單獨重跑跨客戶／客服的瀏覽器驗收：

```sh
npm ci
npx playwright install chromium
npm run test:acceptance
```

測試讀取 `.env.acceptance`，會建立真實客服單、上傳及下載附件、驗證其他客戶無權讀取、客服指派與內部備註、回覆、重試防重複、舊版本衝突、結案／重新開啟、搜尋及登出。另一條流程會關閉 JavaScript，再以原生表單完成建立、附檔、回覆與結案。這些測試不能用唯讀預覽代替。測試結束保留客服單作為驗收證據，僅關閉瀏覽器 sessions。

再次檢查後端：

```sh
docker compose -p supportcenter-acceptance -f compose.acceptance.yaml --env-file .env.acceptance --profile verify run --rm verify
```

停止驗收服務而保留資料：

```sh
docker compose -p supportcenter-acceptance -f compose.acceptance.yaml --env-file .env.acceptance stop
```

## 僅查看介面

需要 Go 1.26+。已提交產生的 templ Go 檔與編譯後靜態資源，因此只看預覽不需要 Node。

```sh
go run ./cmd/preview
```

開啟 <http://127.0.0.1:8081>。

- `/tickets`：示範客服單列表。
- `/tickets/new`：建立客服單。
- `/tickets/1048`：客服單對話。
- `/admin/tickets`、`/admin/tickets/1048`：客服介面。
- `/login`：登入畫面。
- `/account`：個人設定、登入及使用紀錄。
- `/notifications`：使用者站內通知。
- `/admin/users`：管理員使用者列表。
- `/admin/automation`：管理員自動回覆設定。
- `/admin/departments`：管理員客服分組。
- `/admin/announcements`：管理員公告設定；其他預覽頁加上 `?announcements=1` 可查看多則公告。
- 預覽使用示範的網站名稱與 Logo；任一頁加上 `?site=text` 查看沒有 Logo 的頁首，`?site=none` 查看完全未設定網站名稱的樣子。

預覽僅綁定 loopback、只提供示範資料，不接受寫入，也不包含在正式 Docker 映像裡。從自己的電腦查看遠端 VPS 預覽可使用 SSH tunnel：

```sh
ssh -L 8081:127.0.0.1:8081 your-user@your-vps
```

## VPS 部署

需要 Docker Engine、Compose v2 和你管理的 Cloudflare Tunnel。正式 Go 服務只綁定 VPS 的 loopback；PostgreSQL 不向主機公開。以下假設 Tunnel 或反向代理把 `support.example.com` 轉到 `http://127.0.0.1:8082`。在切換前，`.env` 的 `SUPPORT_BIND_PORT=8080` 讓正式服務可與使用 `8082` 的驗收站並行驗證。切換時改為 `8082`，不必改 Cloudflare 路由。

1. 準備設定檔，請勿把真實 secret 提交到 Git。本 VPS 若已由維運程序建立 `.env`，保留既有資料庫密碼，不要覆蓋。

   ```sh
   cp .env.example .env
   chmod 600 .env
   openssl rand -hex 32
   ```

   把產生的值設為 `POSTGRES_PASSWORD`（使用 hex 可避免資料庫 URL 的特殊字元問題）。在 `.env` 填入自己的 `SUPPORT_DOMAIN`、`AUTH0_DOMAIN` 與 `AUTH0_CLIENT_ID`，網站名稱與 Logo 見[網站名稱與 Logo](#網站名稱與-logo)。到 Auth0 後台讀取 Client Secret 後，**只在 VPS 終端**執行 `bash scripts/set-auth0-secret.sh` 並貼上；輸入不會顯示，也不會進入 shell history。不要把 secret 貼到聊天、issue 或指令列參數。

2. 在 Auth0 的 **Regular Web Application** 設定以下值，啟用 Authorization Code grant，並確認要使用的登入連線已對這個應用程式開啟。

   | Auth0 欄位 | 值 |
   | --- | --- |
   | Allowed Callback URLs | `https://support.example.com/auth/callback` |
   | Allowed Logout URLs | `https://support.example.com` |
   | Application Login URI（若使用） | `https://support.example.com/login` |

   登入、登出、Email 與 Discord 連結一律使用 `APP_URL`（Compose 由 `SUPPORT_DOMAIN` 組成）；以其他 Host 連進來時也是如此，表單只接受來自 `APP_URL` 的提交。

   登入頁的「註冊」連到 `/signup`，以相同的 PKCE 流程開啟 Auth0 Universal Login 的註冊畫面（`screen_hint=signup`）；需在 Auth0 的資料庫連線保持允許註冊（未勾選 Disable Sign Ups）。

   `AUTH0_DOMAIN` 只填 hostname，不含 `https://` 或尾端 `/`。Client secret 只存於 VPS。登入身分以 Auth0 `sub` 對應，不能靠 email 自動取得客服權限。

3. 設定 Client Secret 後建置並啟動：

   ```sh
   docker compose up -d --build
   docker compose ps
   docker compose logs --tail=100 app
   curl -fsS http://127.0.0.1:8080/healthz
   ```

   啟動時會初始化資料表並檢查 Auth0 discovery。若環境值、資料庫或 Auth0 連線不正確，應用程式會停止而不開放未驗證模式。從本機請求 `/login` 應收到導向 `https://<AUTH0_DOMAIN>/authorize`，回呼網址應為 `https://<SUPPORT_DOMAIN>/auth/callback`。確認後停止驗收 app，在 `.env` 把 `SUPPORT_BIND_PORT` 改為 `8082`，並執行 `docker compose up -d --force-recreate app`。驗收資料庫與附件仍保留；若要回退，先停止正式 app，把 `SUPPORT_BIND_PORT` 改回 `8080`，再啟動驗收 app。公開站點須以真正的 Auth0 帳號完成登入及登出驗收。

4. 客服／管理員先正常登入一次，再於 VPS 指派角色：

   ```sh
   docker compose exec app admin -subject 'auth0|EXACT_USER_ID' -role admin
   docker compose exec app admin -subject 'auth0|ANOTHER_USER_ID' -role support
   ```

   使用 Auth0 後台顯示的完整 User ID。每次請求從資料庫取得角色，因此權限變更不需重新登入。支援 `user`、`support`、`admin`；目前 support/admin 的客服單操作權限相同，角色異動只允許 VPS 操作者透過 CLI 執行。

   同一個人用不同方式登入（例如 Google 與通行密鑰）時，Auth0 會給不同的 User ID。第一次以新方式登入、且 Auth0 回報 Email 已驗證時，若已有同一 Email 且已驗證的帳號，新方式會併入最早的那個帳號，沿用其角色、名稱與客服單，並在該帳號的「使用紀錄」留下「新增登入方式」。Email 未驗證的登入一律建立獨立帳號。因此 Auth0 只應啟用會確實驗證 Email 的登入方式。

### 網站名稱與 Logo

在 `.env` 設定，修改後執行 `docker compose up -d --no-build app` 套用：

| 設定 | 用途 |
| --- | --- |
| `SITE_NAME` | 顯示在「支援中心」前面，例如 `Example Cloud` 會顯示「Example Cloud 支援中心」；也用於頁面標題、登入標題、Email 寄件人名稱與 Discord 通知。最多 60 字。 |
| `SITE_OWNER` | 頁尾的版權所有者，未設定時使用 `SITE_NAME`；兩者都未設定時不顯示頁尾。 |
| `SITE_WEBSITE` | 頁尾連到的官方網站（http 或 https）。 |
| `SITE_LOGO` | 桌機頁首的橫式 Logo，縮放為 48px 高、最寬 240px。 |
| `SITE_ICON` | 手機頁首、登入頁與瀏覽器分頁的正方形圖示。 |

Logo 與圖示接受 PNG、JPEG 或 WebP，每檔最多 1 MiB、每邊 16–4096px。把檔案放在 `compose.yaml` 旁的 `branding/`（唯讀掛載到容器的 `/branding`），例如 `SITE_LOGO=/branding/logo.png`，並讓容器使用者可讀取（`chmod 644`）。檔案在啟動時讀進記憶體，以內容雜湊作為快取版本；檔案不存在或格式不符時服務不會啟動。未設定圖示時使用內建圖示，未設定 Logo 時頁首改顯示圖示與網站名稱。

正式公開環境強制 HTTPS；Tunnel 到 VPS 本機的連線使用 loopback HTTP。session cookie 使用 `Secure`、`HttpOnly`、`SameSite=Lax` 和 `__Host-` 前綴。不要為了以 HTTP 預覽而移除這些設定。

## 本地開發與測試

Go 1.26+、Node 22+、PostgreSQL 16+。執行正式 server 前需要設定 `.env.example` 中列出的環境變數；程式不自動讀取 `.env`，Compose 會負責注入。直接執行 Go 時請由 shell 或服務管理器注入。

```sh
go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
npm ci
npm run build
go test ./...
```

完整資料庫整合測試需要一個測試用 PostgreSQL database，帳號須能建立 schema。測試會建立獨立 schema 並於結束後刪除，不會清空既有表格。

```sh
TEST_DATABASE_URL='postgres://user:password@localhost:5432/support_test?sslmode=disable' go test -race ./...
```

Race detector 需要 C compiler 與 `CGO_ENABLED=1`。未設定 `TEST_DATABASE_URL` 時，整合測試會明確顯示 skip；不能把這種執行視為已完成權限驗證。

```sh
npm ci
npx playwright install chromium
npm run test:ui
```

UI 測試啟動獨立預覽，檢查明暗模式、390px / 1440px、搜尋、篩選、無 JS 導覽、CSP 錯誤與外部資源請求，並將截圖輸出到 `artifacts/`。測試使用示範資料；實際寫入與權限由 Go/PostgreSQL 整合測試驗證。

修改 `.templ` 或 CSS 後：

```sh
go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate
npm run build
```

登入頁的點陣地球由 `scripts/build-globe.mjs` 從 Natural Earth 1:50m 陸地資料產生：陸地與海洋點陣在可快取的 `web/assets/globe.svg`，動態航線與節點在 `web/globe.go`，兩者皆已提交，建置時不需下載。調整視角或點距後重新產生：

```sh
curl -fsSLo /tmp/ne_50m_land.geojson https://raw.githubusercontent.com/nvkelso/natural-earth-vector/v5.1.2/geojson/ne_50m_land.geojson
node scripts/build-globe.mjs /tmp/ne_50m_land.geojson
```

`npm run test:ui` 另包含 `tests/viewports.spec.js`：以 30 種手機、折疊機、平板與桌機尺寸及深淺色檢查所有預覽頁面的橫向溢出、裁切、重疊、觸控目標與 iOS 輸入框縮放，截圖輸出到 `artifacts/viewports/`。

Go dependency 與 npm dependency 均以 lockfile 固定。`web/assets/htmx.min.js` 由 npm build 複製，瀏覽器不依賴 CDN。

## Portal API

`PORTAL_API_SIGNING_KEY`（64 字元 hex）與 `PORTAL_API_HOSTNAMES` 同時設定時，啟用 `/api/portal/v1/*`。每個請求以 HMAC-SHA256 簽名（`X-Portal-*` 標頭，含時間戳、一次性 nonce 與本文雜湊），簽名金鑰等同可代表任何使用者操作，只存在 VPS 的 `.env`。

Portal 使用者以 **Email** 對應帳號（不分大小寫、忽略前後空白），`subject` 為選填：已有相同 Email 的帳號直接沿用，保留原本的 Auth0 subject、角色、名稱與簽名檔；同一 Email 有多個帳號時，優先已驗證的 SSO 帳號。沒有相同 Email 時，建立 `portal:<email>` 帳號（已驗證、角色 user），並把該 Email 的待處理邀請綁定給它。客服 API（`/staff/*`）需要 Portal 傳來 `canStaff: true`，且客服中心裡該帳號角色為 support 或 admin。

客服單詳情的 `participants` 每筆只有 `email`（例如 `[{"email":"name@example.com"}]`），不回傳名稱或使用者 ID；邀請不論對方是否已註冊都回 `200 {"id":…}`。

## 備份與還原

必須一起備份 PostgreSQL 和附件；只備份其中一項無法完整還原。可執行 `bash scripts/backup.sh`：停止 app 寫入、備份資料庫和附件、檢查 dump 與 archive 格式並生成 SHA256SUMS；失敗時也會嘗試恢復原先運行的 app。需要定期將備份加密複製到另一台自己的主機，並依下方步驟演練還原。

以下為手動方式，會暫停應用程式寫入，以保持兩份備份一致。請確認命令成功後再啟動 app。

```sh
mkdir -p backup
chmod 700 backup
docker compose stop app
docker compose exec -T db pg_dump -U support -d support -Fc > backup/database.dump
docker compose run --rm -T --no-deps --entrypoint tar app -C /data/attachments -czf - . > backup/attachments.tar.gz
docker compose start app
```

把備份加密複製到你控制的另一台主機，依實際資料重要性設定備份頻率與保留期。單台 VPS 上的備份無法防範整台主機遺失。

在**隔離的新環境與空 volumes**演練還原，先準備同版程式及 `.env`：

```sh
docker compose build app
docker compose up -d db
# 等待 db healthy 後執行。
docker compose exec -T db pg_restore -U support -d support --no-owner --exit-on-error < backup/database.dump
docker compose run --rm -T --no-deps --entrypoint tar app -C /data/attachments -xzf - < backup/attachments.tar.gz
# 不重用備份中的登入 session。
docker compose exec -T db psql -U support -d support -c 'TRUNCATE sessions, login_flows;'
docker compose up -d
```

還原後實際登入、開啟客服單並下載附件。不要在正式環境使用 `docker compose down -v`；它會刪除持久化 volumes。

## 維運與邊界

- 掛載目錄僅允許應用使用者存取。檔名由隨機 key 決定；Go `os.Root` 限制路徑範圍，下載拒絕 symlink，並強制以附件方式下載。
- MIME、檔案標頭／格式和尺寸檢查不等同防毒。系統不解壓 ZIP、不執行上傳內容；PDF/ZIP 僅作私人下載。
- 登入暫存資料和過期 session 每 15 分鐘清除。應用有查詢 timeout、HTTP timeout 與健康檢查；最多同時處理 4 個表單提交，限制附件處理的記憶體及暫存磁碟使用。
- 一般錯誤會回滾資料庫並清理新附件；COMMIT 確認遺失時保留檔案，由相同提交識別碼重試確認，避免誤刪已提交附件；如果主機在寫入檔案與提交資料庫之間崩潰，可能留下沒有 metadata 的檔案。維護時須比對 `ticket_attachments.storage_key`，確認後才清理。
- 監控磁碟剩餘空間。上傳有單檔、數量與請求限制，但目前沒有每帳號的總儲存配額。
- 列表每頁 20 筆，對話每頁 30 則。主旨搜尋是參數化 ILIKE；大量客服單的搜尋索引與效能調整應依正式資料量與量測決定。
- 這是單台 VPS 架構，沒有跨主機高可用性。尚未以真實 Auth0 租戶做端到端登入驗證，也未宣稱特定負載下的效能 SLA。

登入設計參考 [Auth0 Authorization Code Flow](https://auth0.com/docs/get-started/authentication-and-authorization-flow/authorization-code-flow/add-login-auth-code-flow)；HTMX 配置參考 [官方文件](https://htmx.org/docs/)；模板建置參考 [templ 文件](https://templ.guide/quick-start/installation/)。

## 上線驗收狀態

修改後須在 VPS 執行上述完整整合及瀏覽器測試，另確認備份還原、實際網域 HTTPS 和 Auth0 租戶登入。程式可編譯、單元測試通過或唯讀介面可開啟，都不能取代這些驗收。正式入口使用 `compose.yaml` 與自己的網域。
