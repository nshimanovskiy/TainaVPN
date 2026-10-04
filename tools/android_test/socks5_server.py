#!/usr/bin/env python3
"""Minimal SOCKS5 proxy (username/password, CONNECT only) for CI tests.
Logs every CONNECT target to stdout. Usage: socks5_server.py PORT USER PASS"""
import socket, struct, sys, threading

PORT, USER, PASS = int(sys.argv[1]), sys.argv[2].encode(), sys.argv[3].encode()

def pipe(a, b):
    try:
        while True:
            d = a.recv(65536)
            if not d:
                break
            b.sendall(d)
    except OSError:
        pass
    finally:
        for s in (a, b):
            try: s.shutdown(socket.SHUT_RDWR)
            except OSError: pass

def recvn(c, n):
    b = b""
    while len(b) < n:
        d = c.recv(n - len(b))
        if not d:
            raise OSError("eof")
        b += d
    return b

def handle(c):
    try:
        ver, n = recvn(c, 2)
        methods = recvn(c, n)
        if 2 not in methods:
            c.sendall(b"\x05\xff"); return
        c.sendall(b"\x05\x02")
        _, ul = recvn(c, 2); u = recvn(c, ul)
        pl = recvn(c, 1)[0]; p = recvn(c, pl)
        if (u, p) != (USER, PASS):
            c.sendall(b"\x01\x01"); return
        c.sendall(b"\x01\x00")
        _, cmd, _, atyp = recvn(c, 4)
        if atyp == 1:
            host = socket.inet_ntoa(recvn(c, 4))
        elif atyp == 3:
            host = recvn(c, recvn(c, 1)[0]).decode()
        elif atyp == 4:
            host = socket.inet_ntop(socket.AF_INET6, recvn(c, 16))
        else:
            return
        port = struct.unpack(">H", recvn(c, 2))[0]
        print(f"CONNECT {host}:{port}", flush=True)
        if cmd != 1:
            c.sendall(b"\x05\x07\x00\x01" + b"\x00" * 6); return
        try:
            r = socket.create_connection((host, port), timeout=10)
            r.settimeout(None)
        except OSError as e:
            print(f"  failed: {e}", flush=True)
            c.sendall(b"\x05\x05\x00\x01" + b"\x00" * 6); return
        c.sendall(b"\x05\x00\x00\x01" + b"\x00" * 6)
        threading.Thread(target=pipe, args=(r, c), daemon=True).start()
        pipe(c, r)
    except OSError:
        pass
    finally:
        c.close()

s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", PORT)); s.listen(128)
print(f"socks5 on :{PORT}", flush=True)
while True:
    c, _ = s.accept()
    threading.Thread(target=handle, args=(c,), daemon=True).start()
