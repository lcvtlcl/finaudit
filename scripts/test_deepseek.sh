#!/usr/bin/env bash
# Проверка доступа к DeepSeek API.
# Использование:
#   export DEEPSEEK_API_KEY=sk-...   (или возьмётся из .env)
#   bash scripts/test_deepseek.sh
set -euo pipefail

# подхватываем .env, если есть — только корректные строки ВИД=значение,
# игнорируя комментарии и кривые строки (чтобы не падать на них)
if [ -f .env ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ''|\#*) continue ;;
      *=*) export "${line%%=*}=${line#*=}" ;;
    esac
  done < .env
fi

KEY="${DEEPSEEK_API_KEY:-}"
MODEL="${DEEPSEEK_MODEL:-deepseek-v4-flash}"
BASE="${DEEPSEEK_BASE_URL:-https://api.deepseek.com}"

if [ -z "$KEY" ]; then
  echo "❌ DEEPSEEK_API_KEY не задан. Положи в .env или export DEEPSEEK_API_KEY=sk-..."
  exit 1
fi

echo "→ Модель: $MODEL"
echo "→ Запрос к $BASE/chat/completions ..."

resp=$(curl -sS "$BASE/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $KEY" \
  -d '{
    "model": "'"$MODEL"'",
    "messages": [
      {"role":"system","content":"Ты финансовый аналитик. Отвечай кратко, без жаргона."},
      {"role":"user","content":"Скажи одним предложением: что такое кассовый разрыв?"}
    ],
    "stream": false
  }')

echo "----- Ответ модели -----"
echo "$resp" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d["choices"][0]["message"]["content"]) if "choices" in d else print("❌ Ошибка:", json.dumps(d, ensure_ascii=False, indent=2))'
echo "------------------------"
echo "✅ Если выше осмысленный ответ — ключ и доступ к API рабочие."
