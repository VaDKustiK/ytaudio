#!/usr/bin/env python3
"""Browser regression tests. Requires Playwright and its Chromium installation.
Runs the actual local server with isolated config/cache. Download/network fixtures
are mocked: this script never downloads media or opens an external release URL.
"""
import argparse, datetime, json, os, pathlib, socket, subprocess, tempfile, time, urllib.request
from playwright.sync_api import sync_playwright, expect


def test(binary):
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
                    jobs.append({"id":identifier,"url":body["url"],"format":body["format"],"source":body.get("source","youtube"),"state":"running","message":"Downloading audio","progress":{"stage":"downloading","percent":42.5,"speed":"1.2MiB/s","eta":"00:10"}})
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
                # Opening the update dialog or cancelling it must never start a download.
                update_requests = []
                update_status = {"running": True, "percent": 47, "message": "Downloading v1.2.0…"}
                def install_update(route):
                    if route.request.method == "POST":
                        update_requests.append(route.request.method)
                    fulfill(route, update_status)
                page.route("**/api/app-install", install_update)
                page.route("**/api/app-update", lambda r: fulfill(r, {"current":"v1.1.0", "latest":"v1.2.0", "ready":True, "can_install":True, "notifications":[]}))
                page.locator('[data-page="info"]').click()
                page.locator("#check-app-update").click()
                page.locator("#bell").click()
                page.locator("#bell-install-update").click()
                update_dialog = page.locator("#app-install-dialog")
                expect(update_dialog).to_be_visible()
                update_dialog.get_by_role("button", name="Cancel").click()
                expect(update_dialog).to_be_hidden()
                assert not update_requests
                expect(page.locator("#notifications")).to_be_hidden()
                page.locator("#install-app-update").click()
                update_dialog.get_by_role("button", name="Install and restart").click()
                expect(page.locator("#app-install-progress")).to_have_attribute("value", "47")
                assert len(update_requests) == 1
                update_status.update(running=False, error=True, message="Could not prepare the update.")
                expect(page.locator("#app-install-message")).to_have_text("Could not prepare the update.")
                expect(page.locator("#install-app-update")).to_be_visible()
                page.locator('[data-page="downloader"]').click()
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
                # The second downloader shares settings/tools, but owns its form and results.
                page.locator('[data-page="soundcloud"]').click()
                expect(page.locator("#page-soundcloud")).to_be_visible()
                expect(page.locator("#sc-format")).to_have_value("flac")
                assert page.locator("#sc-format option").all_text_contents()==["MP3","WAV","FLAC","OGG"]
                expect(page.locator("#sc-jobs .job")).to_have_count(0)
                page.evaluate("navigator.clipboard.writeText('https://soundcloud.com/artist/song?si=tracking')")
                page.locator("#sc-paste-url").click()
                expect(page.locator("#sc-url")).to_have_value("https://soundcloud.com/artist/song?si=tracking")
                # Link validation uses the real backend, before any mocked download.
                previous=len(submissions)
                page.locator("#sc-url").fill("https://youtu.be/wrong-tab");page.locator("#sc-download-button").click()
                expect(page.locator("#sc-download-message")).to_contain_text("only SoundCloud")
                assert len(submissions)==previous
                page.locator("#sc-url").fill("https://soundcloud.com/artist");page.locator("#sc-download-button").click()
                expect(page.locator("#sc-download-message")).to_contain_text("profile or page")
                page.locator("#sc-url").fill("https://soundcloud.com/artist/song?si=tracking");page.locator("#sc-download-button").click()
                expect(page.locator("#sc-jobs .job")).to_have_count(1)
                assert submissions[-1]=={"source":"soundcloud","url":"https://soundcloud.com/artist/song","format":"flac","confirm_playlist":False}
                expect(page.locator("#jobs .job")).to_have_count(1)
                expect(page.locator("#sc-queue-count")).to_have_text("1")
                page.locator("#sc-jobs .job button").click();expect(page.locator("#sc-jobs .job")).to_have_class("job cancelled")
                page.locator("#sc-jobs .job button").click();expect(page.locator("#sc-jobs .job")).to_have_count(0)
                # A shared playlist must be confirmed after its redirect is expanded.
                def share_link(route):
                    if "on.soundcloud.com" in route.request.post_data_json["url"]:
                        fulfill(route,{"url":"https://soundcloud.com/artist/sets/album","source":"soundcloud","playlist":True})
                    else:route.continue_()
                page.route("**/api/resolve-link",share_link)
                previous=len(submissions)
                page.locator("#sc-url").fill("https://on.soundcloud.com/Share");page.locator("#sc-download-button").click()
                expect(page.locator("#playlist-dialog")).to_be_visible();page.locator("#playlist-no").click()
                assert len(submissions)==previous
                page.locator("#sc-download-button").click();page.locator("#playlist-yes").click()
                expect(page.locator("#sc-jobs .job")).to_have_count(1)
                assert submissions[-1]["confirm_playlist"] and submissions[-1]["url"].endswith("/sets/album")
                jobs[-1].update(state="failed",message="SoundCloud did not provide a full track without signing in. It may require an account or SoundCloud Go+ subscription, or offer only a preview. ytaudio downloads without an account and does not save previews.",files=[{"id":"saved-file","name":"Saved track.flac"}])
                expect(page.locator("#sc-jobs .job-message")).to_contain_text("without signing in")
                expect(page.locator("#sc-jobs .job-files button")).to_have_text("BPM and key detection")
                # Audio already saved from a partial playlist remains accessible.
                assert page.locator('#page-soundcloud input[type=password]').count()==0
                opened=[]
                page.route("**/api/open-folder",lambda route:(opened.append(True),route.fulfill(status=204)))
                page.locator("#sc-open-folder").click();expect(page.locator("#sc-open-folder")).to_be_enabled()
                page.wait_for_timeout(100);assert opened
                page.locator('[data-page="downloader"]').click()
                page.locator('[data-page="settings"]').click()
                page.locator('[data-page="info"]').click();expect(page.locator("#app-version")).to_have_text("v0.1.0")
                expect(page.locator("#info-heading")).to_have_text("Info")
                page.set_viewport_size({"width":390,"height":844});page.locator('[data-page="downloader"]').click()
                assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
                for width in (390,640,760,1100):
                    page.set_viewport_size({"width":width,"height":800})
                    download_layouts=[]
                    for section in ("settings","info","downloader","soundcloud","analyzer"):
                        page.locator(f'[data-page="{section}"]').click()
                        assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"),(width,section)
                        bell=page.locator("#bell").bounding_box()
                        assert bell["x"]>=0 and bell["x"]+bell["width"]<=width
                        if section in ("downloader","soundcloud"):
                            form=page.locator(f'#page-{section} .download-panel').bounding_box()
                            assert form["y"]>=bell["y"]+bell["height"]+8,(width,section,"bell overlaps download form")
                            download_layouts.append([form[key] for key in ("x","y","width","height")])
                    assert download_layouts[0]==download_layouts[1],(width,"download layouts differ",download_layouts)
                # Unversioned local builds must not appear to be published releases.
                page.route("**/api/diagnostics",lambda r:fulfill(r,{"version":"dev","os":"linux","arch":"amd64","ready":True,"setup":{"running":False,"message":"Download tools are ready","percent":100},"dependencies":[],"last_log":""}))
                page.route("**/api/app-update",lambda r:fulfill(r,{"current":"dev","ready":False,"checking":False,"notifications":[]}))
                page.reload()
                page.locator('[data-page="info"]').click()
                expect(page.locator("#app-version")).to_have_text("Development build")
                expect(page.locator("#app-update-message")).to_have_text("Development build. App update checks are available in release builds.")
                expect(page.locator("#check-app-update")).to_be_hidden()
                expect(page.locator("#install-app-update")).to_be_hidden()
                expect(page.locator("#notification-count")).to_be_hidden()
                page.locator("#bell").click()
                expect(page.locator("#notification-list")).to_contain_text("Development build")
                assert not errors,errors
                browser.close()
                print("PASS: navigation, saved settings, formats, progress, cancel/dismiss, YouTube + SoundCloud tab isolation, link validation, share/playlist confirmation, guest restriction messages, release bell, safe text, dark mode, clipboard success/empty/denied/native bridge, responsive layouts; no JavaScript errors")
        finally:
            process.terminate()
            try:process.wait(timeout=10)
            except subprocess.TimeoutExpired:process.kill();process.wait()

if __name__=="__main__":
    parser=argparse.ArgumentParser();parser.add_argument("--binary",type=pathlib.Path,required=True);args=parser.parse_args();test(args.binary.resolve())
