#!/bin/bash
read -rsp "Telegram Bot Token: " SIGMO_BOT_TOKEN
read -rsp "Sigmo Worker URL: " SIGMO_WORKER_URL
echo
SIGMO_WEBHOOK_SECRET="$(<sigmo-keys/telegram-webhook-secret.txt)"

curl --fail --silent --show-error \
  "https://api.telegram.org/bot${SIGMO_BOT_TOKEN}/setWebhook" \
  --data-urlencode "url=${SIGMO_WORKER_URL}/v1/telegram-updates" \
  --data-urlencode "secret_token=${SIGMO_WEBHOOK_SECRET}" \
  --data-urlencode 'allowed_updates=["message","callback_query"]'

curl --fail --silent --show-error \
  "https://api.telegram.org/bot${SIGMO_BOT_TOKEN}/getWebhookInfo"

unset SIGMO_BOT_TOKEN SIGMO_WEBHOOK_SECRET SIGMO_WORKER_URL
