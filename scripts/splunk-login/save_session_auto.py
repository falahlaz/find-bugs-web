"""Log in to Splunk through corporate SSO and save the API session cookies.

Ported from find-bugs-bot save_session_auto.py. Run by the Go server via
`xvfb-run -a python3 save_session_auto.py` (Chromium needs a display; the
login page blocks plain headless mode). It fills email, employee ID and
password, then waits up to 5 minutes for the 2FA push to be approved.

Environment:
  SPLUNK_URL, SPLUNK_SSO_EMAIL, SPLUNK_SSO_EMPLOYEE_ID, SPLUNK_SSO_PASSWORD
  SPLUNK_API_SESSION_PATH   where to write the cookie JSON (mode 0600)
  SPLUNK_LOGIN_EVIDENCE_DIR optional; failure screenshots go here (mode 0700)

Exit code 0 means the session file was written with every required cookie.
Never print credentials, cookies or the CSRF token: the server keeps this
output for the Splunk panel.
"""

import json
import os
import sys
import tempfile
import time

from playwright.sync_api import sync_playwright

REQUIRED_COOKIES = ("splunkd_8008", "session_id_8008", "splunkweb_csrf_token_8008", "token_key")
MFA_TIMEOUT_MS = 300_000
STEP_TIMEOUT_MS = 30_000


def env(name: str, default: str | None = None) -> str:
    value = os.environ.get(name, default)
    if not value:
        print(f"Missing env var: {name}", file=sys.stderr)
        sys.exit(2)
    return value


def screenshot(page, out_dir: str | None, label: str) -> None:
    if not out_dir:
        return
    try:
        os.makedirs(out_dir, mode=0o700, exist_ok=True)
        path = os.path.join(out_dir, f"sso_error_{label}_{int(time.time())}.png")
        page.screenshot(path=path)
        print(f"Screenshot saved: {os.path.basename(path)}")
    except Exception:
        pass


def wait_for_cookie(context, name: str, timeout_s: int = 300) -> bool:
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        if any(c["name"] == name for c in context.cookies()):
            return True
        time.sleep(2)
    return False


def write_session(path: str, base_url: str, cookies: dict) -> None:
    data = {"base_url": base_url, "cookies": cookies, "csrf_token": cookies.get("splunkweb_csrf_token_8008")}
    directory = os.path.dirname(os.path.abspath(path))
    os.makedirs(directory, mode=0o700, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=directory, prefix=".splunk-session-")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(data, f, indent=2)
        os.chmod(tmp, 0o600)
        os.replace(tmp, path)
    except Exception:
        os.unlink(tmp)
        raise


def login() -> bool:
    url = env("SPLUNK_URL").rstrip("/")
    email = env("SPLUNK_SSO_EMAIL")
    employee_id = env("SPLUNK_SSO_EMPLOYEE_ID")
    password = env("SPLUNK_SSO_PASSWORD")
    session_path = env("SPLUNK_API_SESSION_PATH")
    evidence = os.environ.get("SPLUNK_LOGIN_EVIDENCE_DIR")

    print("Opening Splunk login. Approve the 2FA push on the account owner's phone.")
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=False)
        try:
            context = browser.new_context(viewport={"width": 1280, "height": 720}, ignore_https_errors=True)
            page = context.new_page()
            page.goto(url)

            print("[1/4] Waiting for Microsoft login page")
            try:
                page.wait_for_selector("#i0116", timeout=STEP_TIMEOUT_MS)
            except Exception:
                screenshot(page, evidence, "step1_email_page")
                print("Step 1 failed: email field not found", file=sys.stderr)
                return False
            field = page.locator("#i0116")
            field.click()
            field.fill("")
            field.press_sequentially(email, delay=50)
            page.wait_for_timeout(300)
            page.click("#idSIButton9")

            print("[2/4] Waiting for corporate SSO page")
            try:
                page.wait_for_selector("#username", timeout=STEP_TIMEOUT_MS)
            except Exception:
                screenshot(page, evidence, "step2_sso_page")
                print("Step 2 failed: SSO username field not found", file=sys.stderr)
                return False
            user_input = page.locator("#username")
            user_input.click()
            user_input.fill("")
            user_input.press_sequentially(employee_id, delay=50)
            page.wait_for_timeout(200)
            pw_input = page.locator("#password")
            pw_input.click()
            pw_input.fill("")
            pw_input.press_sequentially(password, delay=50)
            page.wait_for_timeout(200)
            page.click('input[type="submit"]')

            print("[3/4] Waiting for 2FA approval (max 5 minutes)")
            try:
                page.wait_for_selector("#idSIButton9", timeout=MFA_TIMEOUT_MS)
            except Exception:
                screenshot(page, evidence, "step3_mfa_page")
                print("Step 3 failed: 2FA not approved in time", file=sys.stderr)
                return False
            page.click("#idSIButton9")

            print("[4/4] Waiting for Splunk session")
            if not wait_for_cookie(context, "splunkd_8008", timeout_s=300):
                screenshot(page, evidence, "step4_no_session")
                print("Step 4 failed: Splunk session not created", file=sys.stderr)
                return False

            found = {c["name"]: c["value"] for c in context.cookies() if c["name"] in REQUIRED_COOKIES}
            missing = [n for n in REQUIRED_COOKIES if n not in found]
            if missing:
                print(f"Session incomplete, missing: {', '.join(missing)}", file=sys.stderr)
                return False
            write_session(session_path, url, found)
            print("Session saved.")
            return True
        finally:
            browser.close()


if __name__ == "__main__":
    sys.exit(0 if login() else 1)
