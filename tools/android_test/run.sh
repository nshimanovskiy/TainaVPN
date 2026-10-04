#!/usr/bin/env bash
# Android emulator test: VPN + "Private DNS".
# Runs inside reactivecircus/android-emulator-runner. Expects app-debug.apk in the repo root
# and a test SOCKS5 proxy on the host at 10.0.2.2:21080 (up/pp).
#
# VARIANTS (space separated) lets CI compare config variants; "app" = config exactly as the app builds it.
set -u
PKG=com.tainavpn.app
OUT=/tmp/android-test
mkdir -p $OUT
PDNS_MODE="${PDNS_MODE:-hostname}"
VARIANTS="${VARIANTS:-app}"
overall=0

ann() { # annotation with a multi-line body
  local level="$1" title="$2" body
  body=$(head -c 3000 | sed 's/%/%25/g' | sed ':a;N;$!ba;s/\n/%0A/g')
  echo "::$level::$title%0A$body"
}

adb install -r -g app-debug.apk || { echo "::error::install failed"; exit 1; }
adb shell appops set $PKG ACTIVATE_VPN allow
adb shell settings put global private_dns_mode "$PDNS_MODE"
adb shell settings put global private_dns_specifier one.one.one.one
sleep 8

for V in $VARIANTS; do
  echo "================ variant $V (private DNS: $PDNS_MODE) ================"
  VARIANT="$V" node -e '
    const m = require("./ui/app.js");
    const p = { type: "socks", server: "10.0.2.2", port: 21080, username: "up", password: "pp" };
    m._setStore({ profiles: [p], bypassLan: true, mode: "tun" }, "android");
    const cfg = JSON.parse(m.buildConfig(p));
    cfg.log = { level: "debug", timestamp: true };
    const v = process.env.VARIANT;
    const tun = cfg.inbounds[0];
    if (v.includes("ipv4")) { cfg.dns.strategy = "ipv4_only"; }
    if (v.includes("gvisor")) tun.stack = "gvisor";
    if (v.includes("system")) tun.stack = "system";
    if (v.includes("no853")) cfg.route.rules = cfg.route.rules.filter((r) => r.port !== 853);
    require("fs").writeFileSync("/tmp/client.json", JSON.stringify(cfg, null, 2));
  '
  adb push /tmp/client.json /data/local/tmp/config.json >/dev/null
  adb shell "run-as $PKG sh -c 'mkdir -p files && cat /data/local/tmp/config.json > files/config.json && rm -f files/core.log'"
  adb shell am start -W -n $PKG/.MainActivity --ez test_connect true >/dev/null
  sleep 15

  ok=1
  res=""
  for host in example.com google.com wikipedia.org; do
    r=$(adb shell "ping -c 1 -W 5 $host 2>&1" | head -1)
    if echo "$r" | grep -q "PING $host ("; then res="$res $host:ok"; else res="$res $host:FAIL"; ok=0; fi
  done
  adb shell run-as $PKG cat files/core.log > $OUT/core-$V.log 2>/dev/null
  dot_in=$(grep -cE "inbound connection to .*:853" $OUT/core-$V.log)
  dot_out=$(grep -cE "outbound/(direct|socks)\[[a-z]+\]: outbound connection to .*:853" $OUT/core-$V.log)
  summary="variant=$V mode=$PDNS_MODE dns:[$res ] dot_accepted=$dot_in dot_forwarded=$dot_out"
  if [ $ok = 1 ]; then echo "::notice::PASS $summary"; else echo "::error::FAIL $summary"; overall=1; fi
  grep -iE "error|:853|v6|::|reject" $OUT/core-$V.log | grep -v "exchanged A " | tail -25 | ann warning "core log ($V, $PDNS_MODE)"

  adb shell am start -n $PKG/.MainActivity --ez test_disconnect true >/dev/null
  sleep 5
done

adb shell dumpsys notification --noredact > $OUT/notifications.txt 2>&1
if grep -qiE "private.?dns" $OUT/notifications.txt; then
  echo "::warning::Private DNS notification is present after the run ($PDNS_MODE)"
fi
exit $overall
