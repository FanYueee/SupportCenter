# 技術部門郵件通知

管理員先在「管理設定 → 客服分組」把客服加入技術部門。只有已登入、具客服或管理員角色，且 Auth0 電子郵件已驗證的成員會收到通知。

當客戶建立技術支援客服單、客服把類別改為技術支援，或技術部門成員被指派處理技術客服單時，系統建立待寄通知。若客服單改成其他類別、結案，或成員失去技術部門權限，未寄通知會取消。郵件只含客服單編號及登入後才能開啟的連結，不含客戶訊息內容。

目前沒有 SMTP 設定時，通知保留最多 7 天。設定完成並重建 app 容器後，背景工作會寄送仍有效的通知；失敗最多重試 5 次。投遞採至少一次語義，極少數情況下若 SMTP 已接收但連線在回報前中斷，可能重寄。

在 VPS 的 `.env` 設定以下值，檔案權限保持 `600`：

```dotenv
SMTP_HOST=mail.example.com
SMTP_PORT=587
SMTP_FROM=support@example.com
SMTP_USERNAME=support@example.com
SMTP_PASSWORD=你的密碼
SMTP_TLS_MODE=starttls
```

連接埠 `465` 可改用 `SMTP_TLS_MODE=implicit`。外部 SMTP 一律驗證 TLS 憑證，密碼不會寫入日誌。設定後執行 `docker compose up -d --force-recreate app`。可在新技術客服單上測試；若沒有收到信，先確認成員分組與 Auth0 的 email verified 狀態，再查看 `docker compose logs --tail=100 app`。
