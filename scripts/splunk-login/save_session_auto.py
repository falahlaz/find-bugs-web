"""Log in to Splunk through corporate SSO and save the API session cookies.

Ported from find-bugs-bot save_session_auto.py. Run by the Go server via
`xvfb-run -a python3 save_session_auto.py` (Chromium needs a display; the
login page blocks plain headless mode).

The browser profile is kept between runs, so while the Microsoft/SSO sign-in
is still valid ("Stay signed in?" was answered Yes) a re-auth only bounces
through SSO and gets a fresh Splunk session without a 2FA push. Otherwise it
fills email, employee ID and password, then waits up to 5 minutes for the
2FA push to be approved.

Environment:
  SPLUNK_URL, SPLUNK_SSO_EMAIL, SPLUNK_SSO_EMPLOYEE_ID, SPLUNK_SSO_PASSWORD
  SPLUNK_API_SESSION_PATH     where to write the cookie JSON (mode 0600)
  SPLUNK_BROWSER_PROFILE_DIR  optional; persistent Chromium profile (mode 0700),
                              default: splunk-browser-profile next to the session file
  SPLUNK_LOGIN_EVIDENCE_DIR   optional; failure screenshots go here (mode 0700)

Exit code 0 means the session file was written with every required cookie.
Never print credentials, cookies or the CSRF token: the server keeps this
output for the Splunk panel.
"""

import glob
import json
import os
import sys
import tempfile
import time
from urllib.parse import urlparse

from playwright.sync_api import sync_playwright

REQUIRED_COOKIES = ("splunkd_8008", "session_id_8008", "splunkweb_csrf_token_8008", "token_key")
MFA_TIMEOUT_S = 300
STEP_TIMEOUT_S = 30
# How long to wait for the rest of the cookies once splunkd_8008 is set.
COOKIE_SETTLE_S = 15
MFA_ERROR_TEXT = "An error has occurred while checking transaction status"


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


def prepare_profile(path: str) -> None:
    os.makedirs(path, mode=0o700, exist_ok=True)
    os.chmod(path, 0o700)
    # A run killed on timeout leaves Chromium's lock behind; the server only
    # runs one login at a time, so it is always stale here.
    for lock in glob.glob(os.path.join(path, "Singleton*")):
        try:
            os.unlink(lock)
        except OSError:
            pass


def visible(page, selector: str) -> bool:
    try:
        return page.locator(selector).first.is_visible()
    except Exception:
        return False


def type_into(page, selector: str, value: str, delay: int = 50) -> None:
    field = page.locator(selector)
    field.click()
    field.fill("")
    field.press_sequentially(value, delay=delay)


def login() -> bool:
    url = env("SPLUNK_URL").rstrip("/")
    email = env("SPLUNK_SSO_EMAIL")
    employee_id = env("SPLUNK_SSO_EMPLOYEE_ID")
    password = env("SPLUNK_SSO_PASSWORD")
    session_path = env("SPLUNK_API_SESSION_PATH")
    evidence = os.environ.get("SPLUNK_LOGIN_EVIDENCE_DIR")
    profile = os.environ.get("SPLUNK_BROWSER_PROFILE_DIR") or os.path.join(
        os.path.dirname(os.path.abspath(session_path)), "splunk-browser-profile"
    )
    splunk_host = urlparse(url).hostname or ""

    prepare_profile(profile)
    with sync_playwright() as p:
        context = p.chromium.launch_persistent_context(
            profile, headless=False, viewport={"width": 1280, "height": 720}, ignore_https_errors=True
        )
        try:
            # Drop the expired Splunk cookies so only a fresh login counts;
            # the SSO cookies stay and may let us through without 2FA.
            context.clear_cookies(domain=splunk_host)
            page = context.pages[0] if context.pages else context.new_page()
            print("Opening Splunk login.")
            page.goto(url)

            done: set[str] = set()
            deadline = time.time() + STEP_TIMEOUT_S
            splunkd_seen = 0.0
            while time.time() < deadline:
                names = {c["name"] for c in context.cookies() if c["name"] in REQUIRED_COOKIES}
                if names == set(REQUIRED_COOKIES):
                    break
                if "splunkd_8008" in names:
                    splunkd_seen = splunkd_seen or time.time()
                    if time.time() - splunkd_seen > COOKIE_SETTLE_S:
                        missing = ", ".join(n for n in REQUIRED_COOKIES if n not in names)
                        print(f"Session incomplete, missing: {missing}", file=sys.stderr)
                        return False

                if visible(page, f"text={MFA_ERROR_TEXT}"):
                    screenshot(page, evidence, "mfa_error")
                    print("2FA failed: IBM Verify could not check the push (HTTP 400). Try Re-auth again.", file=sys.stderr)
                    return False

                if visible(page, "#i0116"):
                    if "email" in done:
                        screenshot(page, evidence, "email_again")
                        print("Microsoft asked for the email twice", file=sys.stderr)
                        return False
                    print("[login] Microsoft email")
                    type_into(page, "#i0116", email)
                    page.wait_for_timeout(300)
                    page.click("#idSIButton9")
                    done.add("email")
                    deadline = time.time() + STEP_TIMEOUT_S
                elif visible(page, f'[data-test-id="{email}"]'):
                    print("[login] Microsoft account picker")
                    page.click(f'[data-test-id="{email}"]')
                    deadline = time.time() + STEP_TIMEOUT_S
                elif visible(page, "#username"):
                    if "sso" in done:
                        screenshot(page, evidence, "sso_again")
                        print("SSO asked for the password again (wrong credentials?)", file=sys.stderr)
                        return False
                    print("[login] Corporate SSO password")
                    type_into(page, "#username", employee_id)
                    page.wait_for_timeout(200)
                    type_into(page, "#password", password)
                    page.wait_for_timeout(200)
                    page.click('input[type="submit"]')
                    done.add("sso")
                    # The next page is the 2FA push.
                    deadline = time.time() + MFA_TIMEOUT_S
                    print("[login] Waiting for 2FA approval (max 5 minutes). Approve the push on the account owner's phone.")
                elif (
                    visible(page, "#KmsiCheckboxField")
                    or visible(page, "#KmsiDescription")
                    or ("sso" in done and visible(page, "#idSIButton9"))
                ):
                    # "Stay signed in?": Yes keeps the SSO sign-in in the profile.
                    print("[login] Stay signed in: yes")
                    page.click("#idSIButton9")
                    deadline = time.time() + STEP_TIMEOUT_S
                page.wait_for_timeout(1000)
            else:
                label = "mfa_page" if "sso" in done else "timeout"
                screenshot(page, evidence, label)
                if "sso" in done:
                    print("2FA not approved in time", file=sys.stderr)
                else:
                    print(f"Login stuck on {urlparse(page.url).hostname}", file=sys.stderr)
                return False

            found = {c["name"]: c["value"] for c in context.cookies() if c["name"] in REQUIRED_COOKIES}
            write_session(session_path, url, found)
            if done:
                print("Session saved.")
            else:
                print("Session saved (SSO still signed in, no 2FA needed).")
            return True
        finally:
            context.close()


if __name__ == "__main__":
    sys.exit(0 if login() else 1)
