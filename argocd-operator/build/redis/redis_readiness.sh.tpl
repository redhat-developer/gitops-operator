redis_pwd="$(cat /app/config/redis-auth/auth)"
if [ -z "$redis_pwd" ]; then
    echo "Error: Redis password not mounted correctly"
    if [ ! -z "$AUTH" ]; then
        # TODO: Remove. For migration from before 1.21. https://github.com/redhat-developer/gitops-operator/pull/1307
        redis_pwd="$AUTH"
    else
        exit 1
    fi
fi
response=$(
  env REDISCLI_AUTH="${redis_pwd}" redis-cli \
    -h localhost \
    -p 6379 \
{{- if eq .UseTLS "true"}}
    --tls \
    --cacert /app/config/redis/tls/tls.crt \
{{- end}}
    ping
)
if [ "$response" != "PONG" ]; then
  echo "$response"
  exit 1
fi
echo "response=$response"
