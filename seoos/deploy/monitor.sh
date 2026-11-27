#!/usr/bin/env bash
set -euo pipefail

deploy=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
compose=(bash "$deploy/compose.sh")
"${compose[@]}" exec -T api /usr/local/bin/seoos health </dev/null
"${compose[@]}" exec -T worker /usr/local/bin/seoos worker-health </dev/null
"${compose[@]}" exec -T web node -e 'fetch("http://127.0.0.1:3000/seo/login",{signal:AbortSignal.timeout(5000)}).then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))' </dev/null
if [[ -n "${ARTEX_STAGE_HEALTH_URL:-}" ]]; then
    [[ "$ARTEX_STAGE_HEALTH_URL" =~ ^https://[a-zA-Z0-9.-]+(:[0-9]+)?/seo/login$ ]]
    curl --proto '=https' --silent --show-error --fail --max-time 10 "$ARTEX_STAGE_HEALTH_URL" -o /dev/null
fi
printf '%s\n' 'API, worker, and Web are healthy.'
