# Email 通知

設定 SMTP 後，支援中心會寄三種信。寄件地址為 `SMTP_FROM`（例如 `noreply-support@vproxy.cloud`），寄件人名稱依客服單建立時的網域顯示「AtomRoute 支援中心」或「vProxy Cloud 支援中心」，信中的連結也回到同一個網域。每封信都有純文字與 HTML 版本，並標示 `Auto-Submitted: auto-generated` 以避免自動回覆。

## 設定 SMTP

在 VPS 執行（密碼不會顯示，也不會留在指令歷史）：

```sh
cd /opt/supportcenter
bash scripts/set-smtp.sh
docker compose up -d --no-build app
```

腳本會詢問主機、連接埠（587 用 STARTTLS、465 用 TLS）、寄件地址與帳密，寫入 `.env`（權限保持 `600`）。之後到「管理設定 → Email 通知」按「寄測試信給我」確認。外部 SMTP 一律驗證 TLS 憑證，密碼不會寫入日誌。寄件網域請設定好 SPF、DKIM 與 DMARC，以免信件被歸為垃圾郵件。

未設定 SMTP 時不會排入任何客戶通知，設定後也不會補寄先前的事件。

## 客戶通知

- **客服回覆**：真人客服或 AI 公開回覆時，寄給開單者與已加入的參與者。只寄到已驗證的 Email，且對方未在「個人設定」關閉通知。回覆後等 2 分鐘再寄，期間的多則回覆合併成一封；如果對方在最後一則回覆之後自己回覆過，代表已經看到，就不寄。內部備註、自動訊息與客戶之間的回覆不寄。可選擇是否附上最新一則回覆（最多 4,000 字）。
- **邀請參與者**：寄給受邀的 Email，不論對方是否已註冊，內容完全相同。信中只有邀請人的 Email 與單號，不含主旨或內容。只有 Email 已驗證的使用者能觸發；每位使用者每天最多 30 封、每個地址每天最多收到 3 封，超過的邀請仍會建立，只是不寄信。邀請在寄出前被移除時，信件取消。

管理員可在「管理設定 → Email 通知」分別開關以上信件；關閉時，尚未寄出的同類信件一併取消。寄送失敗最多重試 5 次，超過 3 天未寄出的信件不再寄送；設定頁會顯示最近一次成功與錯誤原因，不保存 SMTP 伺服器的回應內容。

## 技術部門通知

管理員先在「管理設定 → 客服分組」把客服加入技術部門。只有已登入、具客服或管理員角色，且 Auth0 電子郵件已驗證的成員會收到通知。

當客戶建立技術支援客服單、客服把類別改為技術支援，或技術部門成員被指派處理技術客服單時，系統建立待寄通知。若客服單改成其他類別、結案，或成員失去技術部門權限，未寄通知會取消。郵件只含客服單編號及登入後才能開啟的連結，不含客戶訊息內容。

目前沒有 SMTP 設定時，通知保留最多 7 天。設定完成並重建 app 容器後，背景工作會寄送仍有效的通知；失敗最多重試 5 次。投遞採至少一次語義，極少數情況下若 SMTP 已接收但連線在回報前中斷，可能重寄。

SMTP 可用上方的 `scripts/set-smtp.sh` 設定，或在 VPS 的 `.env` 手動設定以下值，檔案權限保持 `600`：

```dotenv
SMTP_HOST=mail.example.com
SMTP_PORT=587
SMTP_FROM=support@example.com
SMTP_USERNAME=support@example.com
SMTP_PASSWORD=你的密碼
SMTP_TLS_MODE=starttls
```

連接埠 `465` 可改用 `SMTP_TLS_MODE=implicit`。外部 SMTP 一律驗證 TLS 憑證，密碼不會寫入日誌。設定後執行 `docker compose up -d --force-recreate app`。可在新技術客服單上測試；若沒有收到信，先確認成員分組與 Auth0 的 email verified 狀態，再查看 `docker compose logs --tail=100 app`。
