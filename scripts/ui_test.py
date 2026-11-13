#!/usr/bin/env python3
"""Browser regression tests. Requires Playwright and its Chromium installation.
Runs the actual local server with isolated config/cache. Download/network fixtures
are mocked: this script never downloads media or opens an external release URL.
"""
import argparse, contextlib, datetime, json, os, pathlib, socket, subprocess, tempfile, time, urllib.request
from playwright.sync_api import sync_playwright, expect


def test(binary, screenshots):
    with tempfile.TemporaryDirectory(prefix="ytaudio-ui-") as root:
        root = pathlib.Path(root); output = root / "music"; output.mkdir()
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0)); port = sock.getsockname()[1]
        config = {"output_folder":str(output), "format":"mp3", "proxy":"", "daily_cap":30,"port":port}
        directory=root / "config/ytaudio";directory.mkdir(parents=True);(directory / "config.json").write_text(json.dumps(config))
        env=dict(os.environ,YTAUDIO_AUTO_SETUP="0",YTAUDIO_CONFIG_DIR=str(root / "config"),YTAUDIO_CACHE_DIR=str(root / "cache"))
        # Keep this UI test offline/deterministic: tool-update behavior has its
        # own backend tests. Wait for real startup checks before mocking the UI.
        cache=root / "cache/ytaudio/bin";cache.mkdir(parents=True)
        (cache / ".last-update-check").write_text(datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00","Z"))
        process=subprocess.Popen([str(binary),"--no-browser"],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        try:
            url=f"http://127.0.0.1:{port}"
            for _ in range(100):
                try: urllib.request.urlopen(url+"/api/health",timeout=1);break
                except Exception: time.sleep(.1)
            else:raise AssertionError("local app did not start")
            for _ in range(100):
                status=json.load(urllib.request.urlopen(url+"/api/diagnostics",timeout=5))
                if not status["setup"]["running"]:break
                time.sleep(.1)
            else:raise AssertionError("initial tool checks did not finish")
            with sync_playwright() as p:
                with urllib.request.urlopen(url+"/static/icon.png") as artwork:
                    assert artwork.headers.get_content_type()=="image/png"
                    assert artwork.read()==(pathlib.Path(__file__).resolve().parent.parent / "packaging/ytaudiologo.png").read_bytes()
                browser=p.chromium.launch(headless=True)
                page=browser.new_page(viewport={"width":1200,"height":950},color_scheme="light")
                errors=[];page.on("pageerror",lambda error:errors.append(str(error)))
                jobs=[];submissions=[]
                def fulfill(route,data):route.fulfill(status=200,content_type="application/json",body=json.dumps(data))
                page.route("**/api/diagnostics",lambda r:fulfill(r,{"version":"v0.1.0","os":"linux","arch":"amd64","ready":True,"setup":{"running":False,"message":"Download tools are ready","percent":100},"dependencies":[{"name":n,"ready":True,"version":"test"} for n in ("yt-dlp","ffmpeg","ffprobe","deno")],"last_log":""}))
                page.route("**/api/app-update",lambda r:fulfill(r,{"current":"v0.1.0","latest":"v0.2.0","ready":True,"checking":False,"url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v0.2.0","notifications":[{"version":"v0.2.0","title":"A new release <img src=x onerror=alert(1)>","date":"2026-11-08T12:00:00Z","notes":"Better downloads.","url":"https://github.com/VaDKustiK/ytaudio/releases/tag/v0.2.0"}]}))
                page.route("**/api/jobs",lambda r:fulfill(r,jobs))
                def download(route):
                    body=route.request.post_data_json;submissions.append(body);identifier=str(len(submissions))
                    jobs.append({"id":identifier,"url":body["url"],"format":body["format"],"state":"running","message":"Downloading audio","progress":{"stage":"downloading","percent":42.5,"speed":"1.2MiB/s","eta":"00:10"}})
                    fulfill(route,{"id":identifier})
                page.route("**/api/download",download)
                def cancel(route):
                    identifier=route.request.url.rsplit("/",1)[1]
                    for j in list(jobs):
                        if j["id"]==identifier:
                            if j["state"] in ("running","queued"):j.update(state="cancelled",message="Cancelled")
                            else:jobs.remove(j)
                    route.fulfill(status=204)
                page.route("**/api/jobs/*",cancel)
                page.route("**/api/open-release",lambda r:r.fulfill(status=204))
                page.context.grant_permissions(["clipboard-read", "clipboard-write"], origin=url)
                page.goto(url)
                expect(page.locator("#download-button")).to_be_enabled()
                expect(page.locator("#notification-count")).to_have_text("1")
                assert page.locator(".topbar, #page-label, .brand-dot, .sidebar-caption, .url-field svg").count()==0
                assert page.evaluate("getComputedStyle(document.documentElement).colorScheme")=="dark"
                background=page.evaluate("getComputedStyle(document.documentElement).backgroundColor")
                page.emulate_media(color_scheme="dark")
                assert page.evaluate("getComputedStyle(document.documentElement).backgroundColor")==background
                page.emulate_media(color_scheme="light")
                removed=["Videos and playlists. One download at a time.","Always starts with the best available YouTube audio.","Only download content you have permission to save.","FROM VIDEO TO YOUR LIBRARY","Paste a link, choose a format, and save audio to your computer.","YouTube to audio.","YOUR AUDIO, SIMPLY.","A LITTLE MORE PERSONAL","Set it once. Get on with listening.","EVERYTHING UNDER THE HOOD","Keep things running. Share details when they don’t.","No account, analytics, or hosted download service. Downloads use your own connection."]
                for text in removed:assert text not in page.locator("body").text_content(),text
                page.evaluate("navigator.clipboard.writeText('  https://youtu.be/pasted-link  ')")
                page.locator("#paste-url").click()
                expect(page.locator("#url")).to_have_value("https://youtu.be/pasted-link")
                assert not submissions,"Paste must not start a download"
                page.evaluate("navigator.clipboard.writeText('')")
                page.locator("#paste-url").click()
                expect(page.locator("#download-message")).to_have_text("Clipboard is empty.")
                expect(page.locator("#url")).to_have_value("https://youtu.be/pasted-link")
                page.evaluate("() => {window.savedReadText=navigator.clipboard.readText; navigator.clipboard.readText=async()=>{throw new DOMException('Denied','NotAllowedError')};}")
                page.locator("#paste-url").click()
                expect(page.locator("#download-message")).to_contain_text("using your keyboard")
                expect(page.locator("#paste-url")).to_be_enabled()
                page.evaluate("window.runtime={ClipboardGetText:async()=> 'https://youtu.be/native-link'}")
                page.locator("#paste-url").click()
                expect(page.locator("#url")).to_have_value("https://youtu.be/native-link")
                expect(page.locator("#download-message")).to_be_empty()
                page.evaluate("delete window.runtime;navigator.clipboard.readText=window.savedReadText;delete window.savedReadText")
                page.locator("#bell").click();expect(page.locator("#notifications")).to_be_visible()
                expect(page.locator(".notification-item strong")).to_contain_text("<img")
                assert page.locator("#notifications img").count()==0
                page.keyboard.press("Escape");expect(page.locator("#notifications")).to_be_hidden()
                assert page.locator('#format option[value="mp4"], #default-format option[value="mp4"]').count()==0
                for format in ("mp3","wav","flac","ogg"):
                    page.select_option("#format",format);assert page.locator("#format-help").inner_text()
                page.locator('[data-page="settings"]').click()
                expect(page.locator("#page-settings")).to_be_visible()
                expect(page.locator("#settings-heading")).to_have_text("Settings")
                page.select_option("#default-format","flac");page.locator("#save-settings").click()
                expect(page.locator("#settings-message")).to_have_text("Preferences saved.")
                assert json.loads((directory / "config.json").read_text())["format"]=="flac"
                page.locator('[data-page="downloader"]').click();expect(page.locator("#format")).to_have_value("flac")
                page.locator("#url").fill("https://youtu.be/abc");page.locator("#download-button").click()
                expect(page.locator(".job")).to_have_count(1)
                expect(page.locator(".job-message")).to_contain_text("43%")
                page.locator(".job button").click();expect(page.locator(".job")).to_have_class("job cancelled")
                page.locator(".job button").click();expect(page.locator(".job")).to_have_count(0)
                page.locator("#url").fill("https://youtube.com/playlist?list=PLtest");page.locator("#download-button").click()
                expect(page.locator("#playlist-dialog")).to_be_visible();page.locator("#playlist-no").click();assert len(submissions)==1
                page.locator("#download-button").click();page.locator("#playlist-yes").click()
                expect(page.locator(".job")).to_have_count(1);assert submissions[-1]["confirm_playlist"]
                jobs[0].update(state="complete",message="Audio saved",progress={"stage":"saved","percent":100,"file":"Music <script>.flac"})
                expect(page.locator(".job-title")).to_have_text("Music <script>.flac")
                assert page.locator("#jobs script").count()==0
                screenshots.mkdir(parents=True,exist_ok=True)
                page.screenshot(path=str(screenshots / "downloader-dark-on-light-system.png"),full_page=True)
                page.emulate_media(color_scheme="dark");page.screenshot(path=str(screenshots / "downloader-dark.png"),full_page=True)
                page.locator('[data-page="settings"]').click()
                page.screenshot(path=str(screenshots / "settings-dark.png"),full_page=True)
                page.locator('[data-page="info"]').click();expect(page.locator("#app-version")).to_have_text("v0.1.0")
                expect(page.locator("#info-heading")).to_have_text("Info")
                page.screenshot(path=str(screenshots / "info-dark.png"),full_page=True)
                page.set_viewport_size({"width":390,"height":844});page.locator('[data-page="downloader"]').click()
                assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
                page.screenshot(path=str(screenshots / "mobile-dark.png"),full_page=True)
                for width in (390,640,760,1100):
                    page.set_viewport_size({"width":width,"height":800})
                    for section in ("settings","info","downloader","analyzer"):
                        page.locator(f'[data-page="{section}"]').click()
                        assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"),(width,section)
                        bell=page.locator("#bell").bounding_box()
                        assert bell["x"]>=0 and bell["x"]+bell["width"]<=width
                # Unversioned local builds must not appear to be published releases.
                page.route("**/api/diagnostics",lambda r:fulfill(r,{"version":"dev","os":"linux","arch":"amd64","ready":True,"setup":{"running":False,"message":"Download tools are ready","percent":100},"dependencies":[],"last_log":""}))
                page.route("**/api/app-update",lambda r:fulfill(r,{"current":"dev","ready":False,"checking":False,"notifications":[]}))
                page.reload()
                page.locator('[data-page="info"]').click()
                expect(page.locator("#app-version")).to_have_text("Development build")
                expect(page.locator("#app-update-message")).to_have_text("Development build. App update checks are available in release builds.")
                expect(page.locator("#check-app-update")).to_be_hidden()
                expect(page.locator("#notification-count")).to_be_hidden()
                page.locator("#bell").click()
                expect(page.locator("#notification-list")).to_contain_text("Development build")
                assert not errors,errors
                browser.close()
                print("PASS: navigation, saved settings, formats, progress, cancel/dismiss, playlists, release bell, safe text, dark mode, clipboard success/empty/denied/native bridge, responsive layouts; no JavaScript errors")
        finally:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait()

if __name__=="__main__":
    parser=argparse.ArgumentParser();parser.add_argument("--binary",type=pathlib.Path,required=True);parser.add_argument("--screenshots",type=pathlib.Path,default=pathlib.Path("/tmp/ytaudio-screenshots"));args=parser.parse_args();test(args.binary.resolve(),args.screenshots)
