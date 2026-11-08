#!/usr/bin/env python3
"""Opt-in live extraction test. Supply a YouTube URL you have permission to save.
Uses isolated settings/cache and a temporary output folder; cleans up afterward.
"""
import argparse, json, os, pathlib, re, socket, subprocess, tempfile, time, urllib.request

def test(binary,url,timeout):
    with tempfile.TemporaryDirectory(prefix="ytaudio-live-") as root:
        root=pathlib.Path(root);output=root / "music";output.mkdir()
        with socket.socket() as sock:sock.bind(("127.0.0.1",0));port=sock.getsockname()[1]
        folder=root / "config/ytaudio";folder.mkdir(parents=True);(folder / "config.json").write_text(json.dumps({"output_folder":str(output),"format":"mp3","proxy":"","daily_cap":30,"port":port}))
        env=dict(os.environ,YTAUDIO_CONFIG_DIR=str(root / "config"),YTAUDIO_CACHE_DIR=str(root / "cache"))
        process=subprocess.Popen([str(binary),"--no-browser"],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        base=f"http://127.0.0.1:{port}"
        def request(path,method="GET",body=None,token=""):
            req=urllib.request.Request(base+path,data=json.dumps(body).encode() if body is not None else None,method=method,headers={"Origin":base,"X-Ytaudio-Token":token,"Content-Type":"application/json"})
            with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
        try:
            for _ in range(100):
                try:diagnostics=request("/api/diagnostics");break
                except Exception:time.sleep(.1)
            else:raise RuntimeError("App did not start")
            token=re.search(r'name="ytaudio-token" content="([a-f0-9]+)"',urllib.request.urlopen(base).read().decode())[1]
            for _ in range(60):
                diagnostics=request("/api/diagnostics")
                if diagnostics["ready"] and not diagnostics["setup"]["running"]:break
                time.sleep(.5)
            if not diagnostics["ready"]:raise RuntimeError("Companion tools are not ready: "+json.dumps(diagnostics))
            identifier=request("/api/download","POST",{"url":url,"format":"mp3","confirm_playlist":False},token)["id"]
            deadline=time.monotonic()+timeout
            while time.monotonic()<deadline:
                job=next(j for j in request("/api/jobs") if j["id"]==identifier)
                if job["state"] in ("complete","failed","cancelled"):
                    print(json.dumps(job,indent=2))
                    if job["state"]!="complete":print(request("/api/diagnostics").get("last_log"));return False
                    files=list(output.glob("*.mp3"));assert files and files[0].stat().st_size>0
                    print("PASS: live YouTube link -> saved MP3 through the application API")
                    return True
                time.sleep(1)
            raise TimeoutError("Live extraction did not complete within the test timeout")
        finally:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait()

if __name__=="__main__":
    p=argparse.ArgumentParser();p.add_argument("--binary",type=pathlib.Path,required=True);p.add_argument("--url",required=True);p.add_argument("--timeout",type=int,default=120);a=p.parse_args();raise SystemExit(0 if test(a.binary.resolve(),a.url,a.timeout) else 1)
