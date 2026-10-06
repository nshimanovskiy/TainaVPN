#!/usr/bin/env bash
# End-to-end test of the real desktop binary on a CI runner (Windows via Git Bash, or Ubuntu):
# the exact config the app builds (TUN, strict_route), kill switch on/off, crash + unblock.
# Usage: run.sh <app binary> <python> <platform: windows|linux> [sudo]
set -u
BIN=$1; PY=$2; PLAT=$3; SUDO=${4:-}
W=$(mktemp -d); cd "$W"
CURL=curl; [ "$PLAT" = windows ] && CURL=curl.exe
fail() {
  for f in out*.log; do waitfor "$f" DONE 40; done # the self-test writes its core log when it stops
  { echo "--- upstream log ---"; tail -n 30 up.log 2>/dev/null; for f in out*.log; do echo "--- $f ---"; tail -c 5000 "$f"; done; } > fail.txt
  echo "::error::$*"
  # the log as an annotation too (readable without downloading the job log)
  echo "::error title=details::$(sed 's/%/%25/g' fail.txt | sed ':a;N;$!ba;s/\n/%0A/g' | sed 's/\r//g')"
  $SUDO "$BIN" --selftest-unblock unblock.log >/dev/null 2>&1; exit 1
}

# the purchased proxy stand-in: SOCKS5 with a password on this machine. On Windows it sends
# from the physical interface address, so its own traffic stays out of the VPN
if [ "$PLAT" = windows ]; then
  export SOCKS_BIND=$(powershell.exe -NoProfile -Command "(Get-NetIPConfiguration | Where-Object IPv4DefaultGateway | Select-Object -First 1).IPv4Address.IPAddress" | tr -d '\r\n ')
  echo "proxy stand-in sends from $SOCKS_BIND"
fi
"$PY" "$GITHUB_WORKSPACE/tools/android_test/socks5_server.py" 21080 up pp > up.log 2>&1 &
sleep 2
$CURL -fsS --max-time 15 -x socks5h://up:pp@127.0.0.1:21080 https://1.1.1.1/cdn-cgi/trace -o /dev/null || fail "upstream proxy does not work"

# the app's own config (ui/app.js buildConfig) for this platform, unchanged except one rule:
# the stand-in proxy runs on this machine, so its own traffic must not loop back into the VPN
node -e '
  const m = require(process.env.GITHUB_WORKSPACE + "/ui/app.js");
  const p = { type: "socks", server: "127.0.0.1", port: 21080, username: "up", password: "pp" };
  m._setStore({ profiles: [p], bypassLan: true, mode: "tun" }, process.argv[1]);
  const cfg = JSON.parse(m.buildConfig(p));
  if (!cfg.inbounds[0].strict_route || cfg.inbounds[0].interface_name !== "tainavpn") throw new Error("unexpected config");
  cfg.route.rules.splice(0, 0, { process_path_regex: ["(?i)python"], outbound: "direct" });
  // strict_route on Windows blocks port 53 outside the VPN for other apps, which includes the
  // stand-in proxy here (with a real remote proxy this does not apply): use DNS-over-TLS in the test
  if (process.argv[1] === "windows") cfg.dns.servers[0] = { type: "tls", tag: "remote", server: "1.1.1.1", detour: "proxy" };
  require("fs").writeFileSync("cfg.json", JSON.stringify(cfg, null, 2));
' "$PLAT" || fail "config"

waitfor() { # file, text, seconds
  for _ in $(seq 1 "$3"); do grep -q "$2" "$1" 2>/dev/null && return 0; grep -q FAILED "$1" 2>/dev/null && return 1; sleep 1; done; return 1
}

echo "== 1. VPN (TUN) without kill switch"
$SUDO "$BIN" --selftest cfg.json out1.log 25 &
waitfor out1.log RUNNING 40 || fail "VPN did not start"
sleep 3
$CURL -fsS --max-time 20 https://1.1.1.1/cdn-cgi/trace -o /dev/null 2> curl_err.txt || fail "no internet through the VPN (IP): $(tr -d '\r\n' < curl_err.txt)"
$CURL -fsS --max-time 20 https://www.cloudflare.com/cdn-cgi/trace -o /dev/null 2> curl_err.txt || fail "no internet through the VPN (domain / DNS): $(tr -d '\r\n' < curl_err.txt)"
grep -q "CONNECT 1.1.1.1:443" up.log || fail "traffic did not go through the proxy"
waitfor out1.log DONE 60 || fail "VPN did not stop"
$CURL -fsS --max-time 20 https://1.1.1.1/cdn-cgi/trace -o /dev/null || fail "no internet after disconnect"
echo "ok"

echo "== 2. VPN with kill switch, normal disconnect"
echo '["127.0.0.1","1.1.1.1"]' > hosts.json
$SUDO "$BIN" --selftest cfg.json out2.log 20 "$(cat hosts.json)" &
waitfor out2.log RUNNING 60 || fail "VPN with kill switch did not start"
sleep 3
$CURL -fsS --max-time 20 https://1.1.1.1/cdn-cgi/trace -o /dev/null || fail "no internet through the VPN with kill switch on"
waitfor out2.log DONE 60 || fail "VPN did not stop"
sleep 2
$CURL -fsS --max-time 20 https://1.0.0.1/cdn-cgi/trace -o /dev/null || fail "internet still blocked after a normal disconnect"
echo "ok"

echo "== 3. VPN with kill switch, crash: internet stays blocked until unblock"
$SUDO "$BIN" --selftest cfg.json out3.log 8 "$(cat hosts.json)" crash &
waitfor out3.log DONE 60 || fail "crash run did not finish"
sleep 2
if $CURL -fsS --max-time 8 https://1.0.0.1/cdn-cgi/trace -o /dev/null; then fail "kill switch did not block traffic after the crash"; fi
$SUDO "$BIN" --selftest-unblock unblock.log; cat unblock.log
sleep 2
$CURL -fsS --max-time 20 https://1.0.0.1/cdn-cgi/trace -o /dev/null || fail "internet still blocked after unblock"
echo "ok"
for f in out*.log; do echo "--- $f ---"; head -c 3000 "$f"; echo; done
echo "desktop e2e ok"
