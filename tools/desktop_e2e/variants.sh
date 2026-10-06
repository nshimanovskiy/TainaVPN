#!/usr/bin/env bash
# Diagnostics: which TUN option breaks traffic on this runner. Runs the real app with
# config variants and reports pass/fail per variant (as a notice annotation).
# Usage: variants.sh <app binary> <python> <platform> [sudo]
set -u
BIN=$1; PY=$2; PLAT=$3; SUDO=${4:-}
W=$(mktemp -d); cd "$W"
CURL=curl; [ "$PLAT" = windows ] && CURL=curl.exe
[ "$PLAT" = windows ] && export SOCKS_BIND=$(powershell.exe -NoProfile -Command "(Get-NetIPConfiguration | Where-Object IPv4DefaultGateway | Select-Object -First 1).IPv4Address.IPAddress" | tr -d '\r\n ')
"$PY" "$GITHUB_WORKSPACE/tools/android_test/socks5_server.py" 21080 up pp > up.log 2>&1 &
UP=$!
sleep 2
summary=""
for v in asis asis2 mixed mixed_fw; do
  node -e '
    const m = require(process.env.GITHUB_WORKSPACE + "/ui/app.js");
    const p = { type: "socks", server: "127.0.0.1", port: 21080, username: "up", password: "pp" };
    m._setStore({ profiles: [p], bypassLan: true, mode: "tun" }, process.argv[1]);
    const cfg = JSON.parse(m.buildConfig(p));
    const t = cfg.inbounds[0], v = process.argv[2];
    if (v.includes("no_ifname")) delete t.interface_name;
    if (v.includes("no_strict")) delete t.strict_route;
    if (v.startsWith("mixed")) t.stack = "mixed";
    if (v.startsWith("system")) t.stack = "system";
    cfg.route.rules.splice(0, 0, { process_path_regex: ["(?i)python"], outbound: "direct" });
    if (process.argv[1] === "windows") cfg.dns.servers[0] = { type: "tls", tag: "remote", server: "1.1.1.1", detour: "proxy" };
    require("fs").writeFileSync("cfg.json", JSON.stringify(cfg, null, 2));
  ' "$PLAT" "$v"
  rm -f out.log
  if [ "$PLAT" = windows ]; then
    netsh advfirewall firewall delete rule name=tvpn-test >/dev/null 2>&1
    case $v in *_fw) netsh advfirewall firewall add rule name=tvpn-test dir=in action=allow program="$(cygpath -w "$BIN")" enable=yes >/dev/null ;; esac
  fi
  $SUDO "$BIN" --selftest cfg.json out.log 14 &
  for _ in $(seq 1 30); do grep -qE "RUNNING|FAILED" out.log 2>/dev/null && break; sleep 1; done
  sleep 3
  if [ "$PLAT" = windows ]; then
    route.exe print -4 | grep -E "0\.0\.0\.0|172\.19|1\.1\.1" > route_$v.txt 2>&1
  else
    ip route show table all | head -20 > route_$v.txt 2>&1
  fi
  ip_ok=no; dns_ok=no
  $CURL -sS --max-time 8 https://1.1.1.1/cdn-cgi/trace -o /dev/null 2> err_ip_$v.txt && ip_ok=yes
  $CURL -sS --max-time 8 https://www.cloudflare.com/cdn-cgi/trace -o /dev/null 2> err_dns_$v.txt && dns_ok=yes
  for _ in $(seq 1 40); do grep -q DONE out.log 2>/dev/null && break; sleep 1; done
  cp out.log out_$v.log
  line="$v: ip=$ip_ok dns=$dns_ok | $(head -c 150 err_ip_$v.txt | tr '\r\n' '  ') | routes: $(tr '\r\n' '  ' < route_$v.txt | tr -s ' ' | head -c 400)"
  echo "$line"
  summary="$summary$line%0A"
  sleep 3
done
echo "::notice title=TUN variants ($PLAT)::$summary"
# core log of the unchanged config, filtered
echo "::notice title=core log asis ($PLAT)::$(grep -vE 'DEBUG|python' out_asis.log | grep -iE 'error|warn|tun|route|start' | head -40 | sed 's/%/%25/g' | sed 's/\x1b\[[0-9;]*m//g' | tr -d '\r' | sed ':a;N;$!ba;s/\n/%0A/g')"
echo "::notice title=upstream log ($PLAT)::$(sort up.log | uniq -c | sort -rn | head -15 | tr -d '\r' | sed ':a;N;$!ba;s/\n/%0A/g')"
kill "$UP" 2>/dev/null; [ "$PLAT" = windows ] && taskkill //F //IM python.exe >/dev/null 2>&1
netsh advfirewall firewall delete rule name=tvpn-test >/dev/null 2>&1
exit 0
