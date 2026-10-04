# Fake Telegram Bot API for CI: feeds scripted updates and records the bot's calls to calls.jsonl
import json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
U={"id":777,"username":"tester","first_name":"T","language_code":sys.argv[2] if len(sys.argv)>2 else "ru"}
CH={"id":777,"type":"private"}
script=[
 {"update_id":1,"message":{"message_id":1,"from":U,"chat":CH,"text":"/start"}},
 {"update_id":2,"callback_query":{"id":"c1","from":U,"message":{"message_id":2,"chat":CH},"data":"link"}},
 {"update_id":3,"callback_query":{"id":"c2","from":U,"message":{"message_id":3,"chat":CH},"data":"new-yes"}},
 {"update_id":4,"callback_query":{"id":"c3","from":U,"message":{"message_id":4,"chat":CH},"data":"file:android"}},
 {"update_id":5,"callback_query":{"id":"c4","from":U,"message":{"message_id":5,"chat":CH},"data":"file:android"}},
]
sent=0
class H(BaseHTTPRequestHandler):
    def log_message(self,*a): pass
    def do_POST(self):
        global sent
        method=self.path.rsplit("/",1)[1]
        n=int(self.headers.get("Content-Length") or 0)
        ctype=self.headers.get("Content-Type","")
        if "multipart" in ctype:
            if self.headers.get("Transfer-Encoding","").lower()=="chunked":
                body=b""
                while True:
                    size=int(self.rfile.readline().strip().split(b";")[0],16)
                    if size==0: self.rfile.readline(); break
                    body+=self.rfile.read(size); self.rfile.readline()
            else:
                body=self.rfile.read(n)
            rec={"method":method,"multipart":True,"size":len(body),"has_file":b'filename="Tainavpn-0.0.0-android.apk"' in body}
        else:
            rec={"method":method,**json.loads(self.rfile.read(n) or b"{}")}
        if method!="getUpdates": open("calls.jsonl","a").write(json.dumps(rec,ensure_ascii=False)+"\n")
        if method=="getUpdates":
            import time
            if sent<len(script):
                # one update per poll, with a pause so the bot's 1s anti-flood doesn't drop it
                time.sleep(1.2); res=[script[sent]]; sent+=1
            else:
                time.sleep(2); res=[]
        elif method=="sendDocument": res={"message_id":9,"document":{"file_id":"FILE123"}}
        else: res=True
        out=json.dumps({"ok":True,"result":res}).encode()
        self.send_response(200); self.send_header("Content-Type","application/json"); self.send_header("Content-Length",str(len(out))); self.end_headers(); self.wfile.write(out)
ThreadingHTTPServer(("127.0.0.1",int(sys.argv[1])),H).serve_forever()
